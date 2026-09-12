package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
)

type productRelationshipsRepository interface { ProductRelationships(context.Context,int64)(admin.ProductRelationships,error) }

func productRelationshipsHandler(adminAPI adminHandlers) http.HandlerFunc { return func(response http.ResponseWriter,request *http.Request){
	_,_,ok:=adminAPI.authorize(response,request,admin.PermissionProductsRead);if !ok{return};id,ok:=pathID(response,request);if !ok{return};provider,ok:=adminAPI.repository.(productRelationshipsRepository);if !ok{adminAPI.failed(response,"product relationships unavailable",errors.New("product relationships unavailable"));return};relationships,err:=provider.ProductRelationships(request.Context(),id);if err!=nil{adminAPI.failed(response,"product relationships",err);return};writeJSON(response,http.StatusOK,map[string]any{"relationships":relationships})
} }
