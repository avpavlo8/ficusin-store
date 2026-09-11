package httpapi

import (
 "net/http"
 "net/url"
 "strings"
 "github.com/avpavlo8/ficusin-store/backend/internal/admin"
 "github.com/avpavlo8/ficusin-store/backend/internal/auth"
)

// Authorize bookmarked HTML URLs too. API endpoints still authorize independently.
func guardAdminPages(handlers adminHandlers, next http.Handler) http.Handler {
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path!="/admin" && !strings.HasPrefix(r.URL.Path,"/admin/") { next.ServeHTTP(w,r); return }
  w.Header().Set("Cache-Control","no-store")
  cookie,err:=r.Cookie(auth.CookieName)
  if err!=nil { http.Redirect(w,r,"/login?returnTo="+url.QueryEscape(r.URL.RequestURI()),http.StatusSeeOther);return }
  user,err:=handlers.auth.UserByToken(r.Context(),cookie.Value)
  if err!=nil || user==nil { http.Redirect(w,r,"/login?returnTo=/admin",http.StatusSeeOther);return }
  section:=r.URL.Query().Get("section");if section=="" { section=r.URL.Query().Get("tab") };if section=="" { section=strings.TrimPrefix(r.URL.Path,"/admin/") }
  permission:=admin.PermissionDashboard
  switch section {
  case "analytics":permission=admin.PermissionAnalyticsRead
  case "procurement","marketplaces":permission=admin.PermissionProcurementRead
  case "settings","finance":permission=admin.PermissionIntegrationsEdit
  case "customers":permission=admin.PermissionCustomersRead
  case "orders":permission=admin.PermissionOrdersRead
  case "products","catalog","categories","collections":permission=admin.PermissionProductsRead
  case "returns":permission=admin.PermissionReturnsRead
  }
  if !admin.Can(user.AdminRole,permission) {
   w.Header().Set("Content-Type","text/html; charset=utf-8");w.WriteHeader(http.StatusForbidden)
   _,_ = w.Write([]byte(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Доступ ограничен · Фикусин</title><link rel="stylesheet" href="/admin-access.css"><body><main><h1>Доступ к разделу ограничен</h1><p>Этот раздел доступен владельцу.</p><a href="/admin?section=orders">К заказам</a></main></body></html>`));return
  }
  next.ServeHTTP(w,r)
 })
}
