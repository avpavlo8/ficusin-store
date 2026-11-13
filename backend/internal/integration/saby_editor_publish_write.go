package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Unlike the legacy authorizedJSON helper, a publishing write is attempted
// exactly once. A refresh/replay, transport error or lost response must never
// silently repeat Create. Readback/reconciliation belongs to the durable worker.
func (c *SabyClient) sabyEditorWriteRPCOnce(ctx context.Context, method string, params any, target any) error {
	switch method {
	case "Nomenclature.Create", "Nomenclature.Update", "NomenclatureImages.UploadTemporary", "Nomenclature.Move":
	default:
		return errors.New("Неподдерживаемое изменение карточки СБИС")
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return err
	}
	request := map[string]any{"jsonrpc": "2.0", "protocol": 6, "method": method, "params": params, "id": 1}
	var response sabyRPCResponse
	if _, err = c.requestJSON(ctx, http.MethodPost, c.serviceURL, request, token, &response); err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("Saby RPC %v: %s", response.Error.Code, safeRemoteMessage(response.Error.Message+" "+response.Error.Details))
	}
	if target == nil {
		return nil
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return errors.New("СБИС не вернул результат изменения")
	}
	if err = json.Unmarshal(response.Result, target); err != nil {
		return errors.New("Не удалось прочитать результат изменения СБИС")
	}
	return nil
}
