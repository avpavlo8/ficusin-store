package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/jackc/pgx/v5"
)

type adminRepository interface {
	Dashboard(context.Context) (admin.Dashboard, error)
	ListCustomers(context.Context) ([]admin.Customer, error)
	UpdateCustomer(context.Context, admin.Actor, int64, admin.CustomerUpdate) (admin.Customer, error)
	ListOrders(context.Context) ([]admin.Order, error)
	UpdateOrderStatus(context.Context, admin.Actor, int64, string, string) (admin.Order, error)
	SetDeliveryFee(context.Context, admin.Actor, int64, float64) (admin.Order, error)
	ListAdminCollections(context.Context) ([]admin.AdminCollection, error)
	SetCollectionProducts(context.Context, admin.Actor, int64, []int64) error
	ListProducts(context.Context) ([]admin.Product, error)
	UpdateProduct(context.Context, admin.Actor, int64, admin.ProductUpdate) (admin.Product, error)
	CreateProduct(context.Context, admin.Actor, admin.ProductCreate) (admin.Product, error)
	ImportProducts(context.Context, admin.Actor, admin.ImportRequest) (admin.ImportResult, error)
	SyncProducts(context.Context, admin.Actor, admin.SyncRequest) (admin.SyncResult, error)
	ListCategories(context.Context) ([]admin.Category, error)
	CreateCategory(context.Context, admin.Actor, admin.CategoryCreate) (admin.Category, error)
	UpdateCategory(context.Context, admin.Actor, int64, admin.CategoryUpdate) (admin.Category, error)
	DeleteCategory(context.Context, admin.Actor, int64) error
}

type adminHandlers struct {
	logger     *slog.Logger
	auth       authService
	repository adminRepository
	// payments is nil when the shop takes no card payments; the refund
	// button then answers that it is unavailable rather than crashing.
	payments refundService
}

// refundService is the slice of the payment service the panel needs.
type refundService interface {
	Refund(ctx context.Context, orderID int64) error
}

// pendingPaymentCanceller — та часть платёжного сервиса, которая закрывает
// незавершённый платёж у провайдера.
//
// Отдельным интерфейсом, а не строкой в refundService: панель обязана
// работать и с провайдером, который отменять не умеет, и с магазином без
// оплаты картой вовсе.
type pendingPaymentCanceller interface {
	CancelPending(ctx context.Context, orderID int64) error
}

func newAdminHandlers(
	logger *slog.Logger,
	authentication authService,
	repository adminRepository,
	payments refundService,
) adminHandlers {
	return adminHandlers{
		logger: logger, auth: authentication, repository: repository, payments: payments,
	}
}

func (handlers adminHandlers) dashboard(response http.ResponseWriter, request *http.Request) {
	user, actor, ok := handlers.authorize(response, request, admin.PermissionDashboard)
	if !ok {
		return
	}
	dashboard, err := handlers.repository.Dashboard(request.Context())
	if err != nil {
		handlers.failed(response, "admin dashboard", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"user": user, "role": actor.Role, "permissions": permissionsFor(actor.Role),
		"dashboard": dashboard,
	})
}

func (handlers adminHandlers) customers(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionCustomersRead)
	if !ok {
		return
	}
	customers, err := handlers.repository.ListCustomers(request.Context())
	if err != nil {
		handlers.failed(response, "list admin customers", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"customers": customers})
}

func (handlers adminHandlers) updateCustomer(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionCustomersEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	var update admin.CustomerUpdate
	if decodeJSON(request, &update) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
		return
	}
	if update.AdminRole != nil && (!admin.AssignableRole(*update.AdminRole) || !admin.Can(actor.Role, admin.PermissionRolesEdit)) {
		writeJSON(response, http.StatusForbidden, errorResponse{Error: "Назначать роли может только владелец"})
		return
	}
	if update.RetailDiscountBPS != nil {
		if !admin.Can(actor.Role, admin.PermissionDiscountsEdit) {
			writeJSON(response, http.StatusForbidden, errorResponse{Error: "Менять скидки может только владелец"})
			return
		}
		if *update.RetailDiscountBPS < 0 || *update.RetailDiscountBPS > 10000 {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Скидка должна быть от 0 до 100%"})
			return
		}
	}
	if update.AccountType != nil && *update.AccountType != "retail" && *update.AccountType != "wholesale" {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный тип клиента"})
		return
	}
	if update.WholesaleStatus != nil && !slices.Contains([]string{"not_requested", "pending", "approved", "rejected"}, *update.WholesaleStatus) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный статус опта"})
		return
	}
	customer, err := handlers.repository.UpdateCustomer(request.Context(), actor, id, update)
	if errors.Is(err, admin.ErrForbidden) {
		writeJSON(response, http.StatusForbidden, errorResponse{Error: "Недостаточно прав"})
		return
	}
	if err != nil {
		handlers.failed(response, "update admin customer", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"customer": customer})
}

func (handlers adminHandlers) orders(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionOrdersRead)
	if !ok {
		return
	}
	orders, err := handlers.repository.ListOrders(request.Context())
	if err != nil {
		handlers.failed(response, "list admin orders", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"orders": orders})
}

func (handlers adminHandlers) updateOrder(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionOrdersEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	var body struct {
		Status        string `json:"status"`
		PaymentStatus string `json:"paymentStatus"`
		// DeliveryFee finishes an order the shop could not price itself.
		// A pointer so that "not sent" and "zero, delivery is free" stay
		// different things.
		DeliveryFee *float64 `json:"deliveryFee"`
		// Refund asks to send the customer's money back. Cancelling the
		// order and returning the money are separate decisions: an order
		// can be cancelled for a customer who paid at the counter.
		Refund bool `json:"refund"`
	}
	if decodeJSON(request, &body) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
		return
	}
	if body.Refund {
		if handlers.payments == nil {
			writeJSON(response, http.StatusServiceUnavailable, errorResponse{
				Error: "Возврат недоступен: оплата не настроена",
			})
			return
		}
		if err := handlers.payments.Refund(request.Context(), id); err != nil {
			handlers.logger.Error("refund failed", "error", err, "order_id", id)
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: err.Error()})
			return
		}
		orders, err := handlers.repository.ListOrders(request.Context())
		if err != nil {
			handlers.failed(response, "list orders after refund", err)
			return
		}
		for _, item := range orders {
			if item.ID == id {
				writeJSON(response, http.StatusOK, map[string]any{"order": item})
				return
			}
		}
		writeJSON(response, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if body.DeliveryFee != nil {
		order, err := handlers.repository.SetDeliveryFee(
			request.Context(), actor, id, *body.DeliveryFee,
		)
		if err != nil {
			handlers.failed(response, "set delivery fee", err)
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"order": order})
		return
	}
	if body.Status != "" && !slices.Contains([]string{"new", "confirmed", "assembling", "ready", "shipped", "completed", "cancelled"}, body.Status) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный статус заказа"})
		return
	}
	if body.PaymentStatus != "" && !slices.Contains([]string{"payment_provider_pending", "pending", "paid", "failed", "refunded",
		"on_delivery", "invoice", "cancelled"}, body.PaymentStatus) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный статус оплаты"})
		return
	}
	// Отмена руками менеджера — та же дыра, что и автоотмена, только дверь
	// ходовее. Заказ закрывался у нас, а платёж оставался живым у ЮKassa:
	// покупатель открывал старую ссылку и платил за заказ, которого больше
	// нет. Гасим платёж до того, как закроем заказ.
	//
	// Отказ провайдера останавливает отмену: отменить можно только
	// неоплаченный платёж, значит покупатель либо платит прямо сейчас, либо
	// уже заплатил — и возвращать товар на полку нельзя.
	if body.Status == "cancelled" {
		if canceller, able := handlers.payments.(pendingPaymentCanceller); able {
			if err := canceller.CancelPending(request.Context(), id); err != nil {
				handlers.logger.Error(
					"cancel payment before manual order cancel failed",
					"error", err, "order_id", id,
				)
				writeJSON(response, http.StatusConflict, errorResponse{
					Error: "Платёж по заказу не удалось закрыть — возможно, покупатель как раз платит. Обновите страницу и попробуйте ещё раз",
				})
				return
			}
		}
	}
	order, err := handlers.repository.UpdateOrderStatus(request.Context(), actor, id, body.Status, body.PaymentStatus)
	if err != nil {
		handlers.failed(response, "update admin order", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"order": order})
}

func (handlers adminHandlers) products(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionProductsRead)
	if !ok {
		return
	}
	products, err := handlers.repository.ListProducts(request.Context())
	if err != nil {
		handlers.failed(response, "list admin products", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"products": products})
}

func (handlers adminHandlers) categoryAttributes(response http.ResponseWriter, request *http.Request) {
	_, _, ok := handlers.authorize(response, request, admin.PermissionProductsRead)
	if !ok { return }
	id, ok := pathID(response, request)
	if !ok { return }
	provider, ok := handlers.repository.(interface {
		ListCategoryAttributes(context.Context, int64) ([]admin.CategoryAttribute, error)
	})
	if !ok { handlers.failed(response, "category attributes unavailable", errors.New("category attributes unavailable")); return }
	items, err := provider.ListCategoryAttributes(request.Context(), id)
	if err != nil { handlers.failed(response, "list category attributes", err); return }
	writeJSON(response, http.StatusOK, map[string]any{"attributes": items})
}

func (handlers adminHandlers) catalogMediaHealth(response http.ResponseWriter,request *http.Request){
	_,_,ok:=handlers.authorize(response,request,admin.PermissionProductsRead);if !ok{return}
	provider,ok:=handlers.repository.(interface{CatalogMediaHealth(context.Context)(admin.MediaHealth,error)});if !ok{handlers.failed(response,"catalog media health unavailable",errors.New("catalog media health unavailable"));return}
	result,err:=provider.CatalogMediaHealth(request.Context());if err!=nil{handlers.failed(response,"catalog media health",err);return};writeJSON(response,http.StatusOK,map[string]any{"media":result})
}

func (handlers adminHandlers) updateProduct(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionProductsEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	var update admin.ProductUpdate
	if decodeJSON(request, &update) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
		return
	}
	if update.Status != nil && !slices.Contains([]string{"draft", "published", "archived"}, *update.Status) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный статус товара"})
		return
	}
	if update.PriceMinor != nil && *update.PriceMinor < 0 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Цена не может быть отрицательной"})
		return
	}
	if update.WholesaleMinQty != nil && *update.WholesaleMinQty < 1 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Минимум для опта должен быть не меньше 1"})
		return
	}
	if update.CatalogSection != nil && !slices.Contains([]string{"plants", "soil", "fertilizer", "pots", "accessories"}, *update.CatalogSection) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный раздел каталога"})
		return
	}
	optionalAttributes := []struct {
		value   *string
		allowed []string
	}{
		{update.PlantKind, []string{"aglaonema", "alocasia", "pineapple", "bonsai"}},
		{update.LightLevel, []string{"sunny", "diffused", "low_light"}},
		{update.Watering, []string{"frequent", "moderate", "rare"}},
		{update.HeightClass, []string{"low", "medium", "high"}},
		{update.CareLevel, []string{"easy", "medium", "demanding"}},
		{update.Placement, []string{"bathroom", "bedroom", "office", "nursery"}},
		{update.PetSafety, []string{"safe", "caution"}},
		{update.GrowthHabit, []string{"compact", "upright", "trailing", "climbing"}},
	}
	for _, attribute := range optionalAttributes {
		if attribute.value != nil && *attribute.value != "" && !slices.Contains(attribute.allowed, *attribute.value) {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректное значение характеристики товара"})
			return
		}
	}
	product, err := handlers.repository.UpdateProduct(request.Context(), actor, id, update)
	if err != nil {
		handlers.failed(response, "update admin product", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"product": product})
}

func (handlers adminHandlers) createProduct(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionProductsEdit)
	if !ok {
		return
	}
	var input admin.ProductCreate
	if decodeJSON(request, &input) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
		return
	}
	if strings.TrimSpace(input.Name) == "" {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Укажите название товара"})
		return
	}
	if input.CatalogSection != "" && !slices.Contains(
		[]string{"plants", "soil", "fertilizer", "pots", "accessories"}, input.CatalogSection) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный раздел каталога"})
		return
	}
	product, err := handlers.repository.CreateProduct(request.Context(), actor, input)
	if err != nil {
		handlers.failed(response, "create admin product", err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{"product": product})
}

// importProducts заводит карточки по списку кодов товаров из СБИС. С
// dryRun панель показывает, что получится, ничего не создавая: список из
// сотни кодов стоит сначала увидеть глазами.
func (handlers adminHandlers) importProducts(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionProductsEdit)
	if !ok {
		return
	}
	var body admin.ImportRequest
	if decodeJSON(request, &body) != nil || len(body.Codes) == 0 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Вставьте коды товаров"})
		return
	}
	if len(body.Codes) > 1000 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "За раз не больше 1000 кодов"})
		return
	}
	result, err := handlers.repository.ImportProducts(request.Context(), actor, body)
	if err != nil {
		handlers.failed(response, "import admin products", err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

type draftProductMerger interface { MergeDraftProducts(context.Context, admin.Actor, admin.MergeProductsRequest) error }

func (handlers adminHandlers) mergeProducts(response http.ResponseWriter, request *http.Request) {
	_,actor,ok:=handlers.authorize(response,request,admin.PermissionProductsEdit);if !ok{return}
	var body admin.MergeProductsRequest
	if decodeJSON(request,&body)!=nil || body.TargetProductID<=0 || len(body.SourceProductIDs)==0 || len(body.SourceProductIDs)>100 { writeJSON(response,http.StatusBadRequest,errorResponse{Error:"Выберите основную карточку и черновики"});return }
	merger,ok:=handlers.repository.(draftProductMerger);if !ok{writeJSON(response,http.StatusNotImplemented,errorResponse{Error:"Объединение недоступно"});return}
	if err:=merger.MergeDraftProducts(request.Context(),actor,body);err!=nil{handlers.failed(response,"merge draft products",err);return}
	writeJSON(response,http.StatusOK,map[string]any{"ok":true})
}

func (handlers adminHandlers) syncProducts(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionProductsSync)
	if !ok {
		return
	}
	var body admin.SyncRequest
	if decodeJSON(request, &body) != nil || len(body.ProductIDs) == 0 || len(body.ProductIDs) > 500 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите от 1 до 500 товаров"})
		return
	}
	allowedFields := []string{"name", "photo", "price", "description"}
	if len(body.Fields) == 0 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Выберите поля для синхронизации"})
		return
	}
	for _, field := range body.Fields {
		if !slices.Contains(allowedFields, field) {
			writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректное поле синхронизации"})
			return
		}
	}
	result, err := handlers.repository.SyncProducts(request.Context(), actor, body)
	if err != nil {
		handlers.failed(response, "sync admin products", err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (handlers adminHandlers) categories(response http.ResponseWriter, request *http.Request) {
	_,_,ok:=handlers.authorize(response,request,admin.PermissionProductsRead); if !ok{return}
	items,err:=handlers.repository.ListCategories(request.Context()); if err!=nil{handlers.failed(response,"list categories",err);return}
	writeJSON(response,http.StatusOK,map[string]any{"categories":items})
}

func (handlers adminHandlers) createCategory(response http.ResponseWriter,request *http.Request){
	_,actor,ok:=handlers.authorize(response,request,admin.PermissionProductsEdit); if !ok{return}
	var input admin.CategoryCreate
	if decodeJSON(request,&input)!=nil||strings.TrimSpace(input.Name)==""||strings.TrimSpace(input.Slug)==""{
		writeJSON(response,http.StatusBadRequest,errorResponse{Error:"Укажите название и slug"});return
	}
	item,err:=handlers.repository.CreateCategory(request.Context(),actor,input); if err!=nil{handlers.failed(response,"create category",err);return}
	writeJSON(response,http.StatusCreated,map[string]any{"category":item})
}

func (handlers adminHandlers) updateCategory(response http.ResponseWriter,request *http.Request){
	_,actor,ok:=handlers.authorize(response,request,admin.PermissionProductsEdit); if !ok{return}
	id,ok:=pathID(response,request);if !ok{return};var input admin.CategoryUpdate
	if decodeJSON(request,&input)!=nil{writeJSON(response,http.StatusBadRequest,errorResponse{Error:"Некорректные данные"});return}
	item,err:=handlers.repository.UpdateCategory(request.Context(),actor,id,input);if err!=nil{handlers.failed(response,"update category",err);return}
	writeJSON(response,http.StatusOK,map[string]any{"category":item})
}

func (handlers adminHandlers) deleteCategory(response http.ResponseWriter,request *http.Request){
	_,actor,ok:=handlers.authorize(response,request,admin.PermissionProductsEdit);if !ok{return}
	id,ok:=pathID(response,request);if !ok{return}
	err:=handlers.repository.DeleteCategory(request.Context(),actor,id)
	if errors.Is(err,admin.ErrCategoryNotEmpty){writeJSON(response,http.StatusConflict,errorResponse{Error:"Сначала перенесите товары и удалите вложенные категории"});return}
	if err!=nil{handlers.failed(response,"delete category",err);return}
	writeJSON(response, http.StatusOK, map[string]any{"deleted": true})
}

func (handlers adminHandlers) authorize(
	response http.ResponseWriter,
	request *http.Request,
	permission string,
) (*auth.User, admin.Actor, bool) {
	cookie, err := request.Cookie(auth.CookieName)
	if err != nil {
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return nil, admin.Actor{}, false
	}
	user, err := handlers.auth.UserByToken(request.Context(), cookie.Value)
	if err != nil {
		handlers.failed(response, "admin session lookup", err)
		return nil, admin.Actor{}, false
	}
	if user == nil {
		writeJSON(response, http.StatusUnauthorized, errorResponse{Error: "Требуется авторизация"})
		return nil, admin.Actor{}, false
	}
	// The role comes from admin_users and nothing else. It used to be
	// granted by matching the account's email against a list, but nobody
	// verifies an email address here, and the account owner can change it
	// from the profile page — so that match proved nothing.
	role := user.AdminRole
	if !admin.Can(role, permission) {
		writeJSON(response, http.StatusForbidden, errorResponse{Error: "Недостаточно прав"})
		return nil, admin.Actor{}, false
	}
	return user, admin.Actor{CustomerID: user.ID, Role: role}, true
}

func (handlers adminHandlers) failed(response http.ResponseWriter, operation string, err error) {
	status := http.StatusServiceUnavailable
	if errors.Is(err, pgx.ErrNoRows) {
		status = http.StatusNotFound
	}
	if errors.Is(err, admin.ErrInvalidInput) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, admin.ErrForbidden) {
		status = http.StatusForbidden
	}
	handlers.logger.Error(operation+" failed", "error", err)
	writeJSON(response, status, errorResponse{Error: "Не удалось выполнить операцию"})
}

func pathID(response http.ResponseWriter, request *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(request.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный идентификатор"})
		return 0, false
	}
	return id, true
}

func permissionsFor(role string) []string {
	all := []string{
		admin.PermissionDashboard, admin.PermissionCustomersRead, admin.PermissionCustomersEdit,
		admin.PermissionRolesEdit, admin.PermissionDiscountsEdit, admin.PermissionOrdersRead,
		admin.PermissionOrdersEdit, admin.PermissionProductsRead, admin.PermissionProductsEdit,
		admin.PermissionProductsSync, admin.PermissionProcurementRead,
		admin.PermissionProcurementEdit, admin.PermissionIntegrationsEdit,
	}
	result := make([]string, 0, len(all))
	for _, permission := range all {
		if admin.Can(role, permission) {
			result = append(result, permission)
		}
	}
	return result
}

func (handlers adminHandlers) collections(response http.ResponseWriter, request *http.Request) {
	if _, _, ok := handlers.authorize(response, request, admin.PermissionProductsRead); !ok {
		return
	}
	collections, err := handlers.repository.ListAdminCollections(request.Context())
	if err != nil {
		handlers.failed(response, "list collections", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"collections": collections})
}

func (handlers adminHandlers) updateCollection(response http.ResponseWriter, request *http.Request) {
	_, actor, ok := handlers.authorize(response, request, admin.PermissionProductsEdit)
	if !ok {
		return
	}
	id, ok := pathID(response, request)
	if !ok {
		return
	}
	var body struct {
		Products []int64 `json:"products"`
	}
	if decodeJSON(request, &body) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный список товаров"})
		return
	}
	if err := handlers.repository.SetCollectionProducts(
		request.Context(), actor, id, body.Products,
	); err != nil {
		handlers.failed(response, "set collection products", err)
		return
	}
	collections, err := handlers.repository.ListAdminCollections(request.Context())
	if err != nil {
		handlers.failed(response, "list collections", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"collections": collections})
}
