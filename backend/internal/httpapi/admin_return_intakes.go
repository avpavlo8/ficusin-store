package httpapi

import (
	"context"
	"errors"
	"github.com/avpavlo8/ficusin-store/backend/internal/admin"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
)

type returnIntakeRepository interface {
	ReturnIntakeDraft(context.Context, admin.Actor) (*admin.ReturnIntake, error)
	ReturnIntake(context.Context, admin.Actor, string) (*admin.ReturnIntake, error)
	SaveReturnIntakeDraft(context.Context, admin.Actor, string, admin.ReturnIntakeSave) (*admin.ReturnIntake, error)
	AcceptReturnIntake(context.Context, admin.Actor, string, int64, bool) (*admin.ReturnIntake, error)
}

func (handlers adminHandlers) intakeRepository(response http.ResponseWriter, request *http.Request, permission string) (returnIntakeRepository, admin.Actor, bool) {
	_, actor, ok := handlers.authorize(response, request, permission)
	if !ok {
		return nil, actor, false
	}
	repository, ok := handlers.repository.(returnIntakeRepository)
	if !ok {
		writeJSON(response, http.StatusServiceUnavailable, errorResponse{Error: "Черновики возвратов недоступны"})
	}
	return repository, actor, ok
}
func intakeResponse(response http.ResponseWriter, intake *admin.ReturnIntake, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(response, http.StatusNotFound, errorResponse{Error: "Черновик не найден"})
		return
	}
	if err != nil {
		writeJSON(response, http.StatusConflict, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"intake": intake})
}
func (handlers adminHandlers) returnIntakeDraft(response http.ResponseWriter, request *http.Request) {
	repository, actor, ok := handlers.intakeRepository(response, request, admin.PermissionReturnsEdit)
	if !ok {
		return
	}
	intake, err := repository.ReturnIntakeDraft(request.Context(), actor)
	intakeResponse(response, intake, err)
}
func (handlers adminHandlers) returnIntake(response http.ResponseWriter, request *http.Request) {
	repository, actor, ok := handlers.intakeRepository(response, request, admin.PermissionReturnsEdit)
	if !ok {
		return
	}
	intake, err := repository.ReturnIntake(request.Context(), actor, request.PathValue("intakeID"))
	intakeResponse(response, intake, err)
}
func (handlers adminHandlers) saveReturnIntakeDraft(response http.ResponseWriter, request *http.Request) {
	repository, actor, ok := handlers.intakeRepository(response, request, admin.PermissionReturnsEdit)
	if !ok {
		return
	}
	var input admin.ReturnIntakeSave
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректные данные возврата"})
		return
	}
	intake, err := repository.SaveReturnIntakeDraft(request.Context(), actor, request.PathValue("intakeID"), input)
	intakeResponse(response, intake, err)
}
func (handlers adminHandlers) acceptReturnIntake(response http.ResponseWriter, request *http.Request) {
	receipt := strings.HasSuffix(request.URL.Path, "/receipt")
	permission := admin.PermissionReturnsEdit
	if receipt {
		permission = admin.PermissionReturnsReceipt
	}
	repository, actor, ok := handlers.intakeRepository(response, request, permission)
	if !ok {
		return
	}
	var input struct {
		Revision int64 `json:"revision"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(response, http.StatusBadRequest, errorResponse{Error: "Некорректная версия возврата"})
		return
	}
	intake, err := repository.AcceptReturnIntake(request.Context(), actor, request.PathValue("intakeID"), input.Revision, receipt)
	intakeResponse(response, intake, err)
}
