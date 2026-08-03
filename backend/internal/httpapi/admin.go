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
	ListProducts(context.Context) ([]admin.Product, error)
	UpdateProduct(context.Context, admin.Actor, int64, admin.ProductUpdate) (admin.Product, error)
	SyncProducts(context.Context, admin.Actor, admin.SyncRequest) (admin.SyncResult, error)
	ListCategories(context.Context) ([]admin.Category, error)
	CreateCategory(context.Context, admin.Actor, admin.CategoryCreate) (admin.Category, error)
	UpdateCategory(context.Context, admin.Actor, int64, admin.CategoryUpdate) (admin.Category, error)
	DeleteCategory(context.Context, admin.Actor, int64) error
}

type adminHandlers struct {
	logger      *slog.Logger
	auth        authService
	repository  adminRepository
	ownerEmails map[string]struct{}
}

func newAdminHandlers(
	logger *slog.Logger,
	authentication authService,
	repository adminRepository,
	ownerEmails map[string]struct{},
) adminHandlers {
	return adminHandlers{logger: logger, auth: authentication, repository: repository, ownerEmails: ownerEmails}
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
	}
	if decodeJSON(request, &body) != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные"})
		return
	}
	if body.Status != "" && !slices.Contains([]string{"new", "confirmed", "assembling", "ready", "shipped", "completed", "cancelled"}, body.Status) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный статус заказа"})
		return
	}
	if body.PaymentStatus != "" && !slices.Contains([]string{"payment_provider_pending", "pending", "paid", "failed", "refunded"}, body.PaymentStatus) {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректный статус оплаты"})
		return
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
	allowedFields := []string{"name", "photo", "price", "dimensions", "description"}
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
	role := user.AdminRole
	if _, owner := handlers.ownerEmails[strings.ToLower(user.Email)]; owner {
		role = admin.RoleOwner
	}
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
		admin.PermissionProductsSync, admin.PermissionIntegrationsEdit,
	}
	result := make([]string, 0, len(all))
	for _, permission := range all {
		if admin.Can(role, permission) {
			result = append(result, permission)
		}
	}
	return result
}
