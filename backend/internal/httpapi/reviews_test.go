package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avpavlo8/ficusin-store/backend/internal/auth"
	"github.com/avpavlo8/ficusin-store/backend/internal/reviews"
)

type reviewStoreStub struct { createID int64; createErr error; input reviews.Input; customerID int64; slug string }
func (stub *reviewStoreStub) Create(_ context.Context, customerID int64, slug string, input reviews.Input)(int64,error){stub.input=input;stub.customerID=customerID;stub.slug=slug;return stub.createID,stub.createErr}
func (*reviewStoreStub) Photo(context.Context,int64)(string,[]byte,error){return "image/jpeg",[]byte("photo"),nil}
func (*reviewStoreStub) Pending(context.Context)([]reviews.ModerationItem,error){return nil,nil}
func (*reviewStoreStub) Moderate(context.Context,int64,int64,string)error{return nil}

func TestCreateReviewRequiresCompletedPurchase(t *testing.T){
	store:=&reviewStoreStub{createErr:reviews.ErrNotPurchased}
	request:=httptest.NewRequest(http.MethodPost,"/api/v1/products/ficus/reviews",strings.NewReader(`{"rating":5,"text":"Здоровое растение"}`))
	request.Header.Set("Content-Type","application/json");request.AddCookie(&http.Cookie{Name:auth.CookieName,Value:"session"})
	response:=httptest.NewRecorder();dependencies:=testDependencies(catalogStub{},authStub{user:&auth.User{ID:42}});dependencies.Reviews=store
	NewRouter(discardLogger(),dependencies).ServeHTTP(response,request)
	if response.Code!=http.StatusForbidden{t.Fatalf("status=%d want %d",response.Code,http.StatusForbidden)}
}

func TestCreateReviewIsPendingAndCarriesVerifiedCustomer(t *testing.T){
	store:=&reviewStoreStub{createID:7}
	request:=httptest.NewRequest(http.MethodPost,"/api/v1/products/ficus/reviews",strings.NewReader(`{"rating":4,"text":"Хорошо упаковано"}`));request.Header.Set("Content-Type","application/json");request.AddCookie(&http.Cookie{Name:auth.CookieName,Value:"session"})
	response:=httptest.NewRecorder();dependencies:=testDependencies(catalogStub{},authStub{user:&auth.User{ID:42}});dependencies.Reviews=store
	NewRouter(discardLogger(),dependencies).ServeHTTP(response,request)
	if response.Code!=http.StatusCreated{t.Fatalf("status=%d body=%s",response.Code,response.Body.String())};if store.customerID!=42||store.slug!="ficus"||store.input.Rating!=4{t.Fatalf("unexpected call: %#v",store)};if !strings.Contains(response.Body.String(),`"status":"pending"`){t.Fatalf("body=%s",response.Body.String())}
}
