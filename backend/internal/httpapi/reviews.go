package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/avpavlo8/ficusin-store/backend/internal/reviews"
)

type reviewStore interface { Create(context.Context,int64,string,reviews.Input)(int64,error); Photo(context.Context,int64)(string,[]byte,error); ModerationMedia(context.Context,int64)(string,[]byte,error); Pending(context.Context)([]reviews.ModerationItem,error); Moderate(context.Context,int64,int64,string)error }

func createReviewHandler(logger *slog.Logger, authentication authService, store reviewStore) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){
	if store==nil { writeJSON(w,http.StatusServiceUnavailable,errorResponse{Error:"Отзывы временно недоступны"}); return }
	cookie,err:=r.Cookie(auth.CookieName); if err!=nil { writeJSON(w,http.StatusUnauthorized,errorResponse{Error:"Войдите в аккаунт"}); return }; user,err:=authentication.UserByToken(r.Context(),cookie.Value); if err!=nil||user==nil { writeJSON(w,http.StatusUnauthorized,errorResponse{Error:"Войдите в аккаунт"}); return }
	var input reviews.Input; if err:=decodeJSONWithLimit(r,&input,35*1024*1024); err!=nil { writeJSON(w,http.StatusBadRequest,errorResponse{Error:"Некорректные данные отзыва"}); return }; id,err:=store.Create(r.Context(),user.ID,r.PathValue("slug"),input); if errors.Is(err,reviews.ErrNotPurchased){writeJSON(w,http.StatusForbidden,errorResponse{Error:"Отзыв доступен после завершённой покупки этого товара"});return}; if errors.Is(err,reviews.ErrAlreadyReviewed){writeJSON(w,http.StatusConflict,errorResponse{Error:"Вы уже оставили отзыв на эту покупку"});return}; if err!=nil{logger.Error("create review failed","error",err);writeJSON(w,http.StatusBadRequest,errorResponse{Error:"Проверьте оценку, текст и медиафайлы"});return}; writeJSON(w,http.StatusCreated,map[string]any{"id":id,"status":"pending"})
} }

func reviewPhotoHandler(store reviewStore) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){ id,err:=strconv.ParseInt(r.PathValue("id"),10,64); if err!=nil||store==nil{http.NotFound(w,r);return}; kind,data,err:=store.Photo(r.Context(),id);if err!=nil{http.NotFound(w,r);return};w.Header().Set("Content-Type",kind);w.Header().Set("Cache-Control","public, max-age=86400");http.ServeContent(w,r,"review-media",time.Time{},bytes.NewReader(data)) } }
func moderationMediaHandler(handlers adminHandlers,store reviewStore) http.HandlerFunc { return func(w http.ResponseWriter,r *http.Request){_,_,ok:=handlers.authorize(w,r,admin.PermissionProductsRead);if !ok{return};id,err:=strconv.ParseInt(r.PathValue("id"),10,64);if err!=nil{http.NotFound(w,r);return};kind,data,err:=store.ModerationMedia(r.Context(),id);if err!=nil{http.NotFound(w,r);return};w.Header().Set("Content-Type",kind);w.Header().Set("Cache-Control","private, no-store");http.ServeContent(w,r,"review-media",time.Time{},bytes.NewReader(data))} }

func pendingReviewsHandler(handlers adminHandlers,store reviewStore) http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){_,_,ok:=handlers.authorize(w,r,admin.PermissionProductsRead);if !ok{return};items,err:=store.Pending(r.Context());if err!=nil{handlers.failed(w,"list pending reviews",err);return};writeJSON(w,http.StatusOK,map[string]any{"reviews":items})}}
func moderateReviewHandler(handlers adminHandlers,store reviewStore) http.HandlerFunc{return func(w http.ResponseWriter,r *http.Request){user,_,ok:=handlers.authorize(w,r,admin.PermissionProductsEdit);if !ok{return};id,err:=strconv.ParseInt(r.PathValue("id"),10,64);var body struct{Status string `json:"status"`};if err!=nil||decodeJSON(r,&body)!=nil{writeJSON(w,http.StatusBadRequest,errorResponse{Error:"Некорректные данные"});return};if err:=store.Moderate(r.Context(),id,user.ID,body.Status);err!=nil{handlers.failed(w,"moderate review",err);return};writeJSON(w,http.StatusOK,map[string]any{"status":body.Status})}}
