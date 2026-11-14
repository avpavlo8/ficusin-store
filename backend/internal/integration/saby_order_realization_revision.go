package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/avpavlo8/ficusin-store/backend/internal/ordersale"
)

// A revision never creates a document and never rewrites the origin receipt.
// Each next mutation is returned to the durable worker before it is sent.
type sabyRealizationRevisionReceipt struct {
	Version           int                              `json:"version"`
	RevisionID        int64                            `json:"revisionId"`
	CurrentRevisionID int64                            `json:"currentRevisionId"`
	OriginHash        string                           `json:"originHash"`
	CurrentHash       string                           `json:"currentHash"`
	TargetHash        string                           `json:"targetHash"`
	Phase             string                           `json:"phase"`
	OriginalLines     []SabyOrderRealizationPricedLine `json:"originalLines,omitempty"`
	KnownLines        []SabyOrderRealizationPricedLine `json:"knownLines,omitempty"`
	RetiredLines      []SabyOrderRealizationPricedLine `json:"retiredLines,omitempty"`
	DeleteRowID       int64                            `json:"deleteRowId,omitempty"`
	Prepared          *SabyOrderRealizationPreparedRow `json:"prepared,omitempty"`
	SavedRowID        int64                            `json:"savedRowId,omitempty"`
	LineIndex         int                              `json:"lineIndex"`
	UnpostVerified    bool                             `json:"unpostVerified"`
	SawPosted         bool                             `json:"sawPosted"`
	TargetPostSent    bool                             `json:"targetPostSent"`
	TargetSawPosted   bool                             `json:"targetSawPosted"`
	Polls             int                              `json:"polls"`
	LastReceipt       json.RawMessage                  `json:"lastReceipt,omitempty"`
}

func sabyRevisionHash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func sabyRevisionSameIdentity(a, b ordersale.Request) bool {
	return a.IntentID == b.IntentID && a.OperationKey == b.OperationKey && a.ExecutionMode == "live" && b.ExecutionMode == "live" &&
		a.Event.OrderID == b.Event.OrderID && a.BuyerID == b.BuyerID &&
		a.SiteWarehouseID == b.SiteWarehouseID && a.NativeWarehouseID == b.NativeWarehouseID && a.MovementWarehouseID == b.MovementWarehouseID &&
		a.OrganizationID == b.OrganizationID && a.RegulationID == b.RegulationID
}

func sabyRevisionIDs(lines []SabyOrderRealizationPricedLine) []int64 {
	ids := make([]int64, 0, len(lines))
	for _, line := range lines {
		ids = append(ids, line.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func sabyRevisionExactIDs(ids []int64, lines []SabyOrderRealizationPricedLine) bool {
	if len(ids) != len(lines) {
		return false
	}
	wanted := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || wanted[id] {
			return false
		}
		wanted[id] = true
	}
	for _, line := range lines {
		if !wanted[line.ID] {
			return false
		}
		delete(wanted, line.ID)
	}
	return len(wanted) == 0
}

func sabyRevisionPostLines(lines []SabyOrderRealizationFillLine) []SabyOrderRealizationPostLine {
	out := make([]SabyOrderRealizationPostLine, 0, len(lines))
	for _, line := range lines {
		out = append(out, SabyOrderRealizationPostLine{NomenclatureID: line.NomenclatureID, Quantity: line.Quantity, UnitPrice: line.UnitPrice})
	}
	return out
}

func sabyRevisionOpen(snapshot SabyOrderRealizationSnapshot, lines []SabyOrderRealizationPricedLine, explicit bool) bool {
	if explicit && !snapshot.PostingStateKnown || snapshot.Posted || snapshot.HasEntries || snapshot.PostingErrors || snapshot.StaleEntries || snapshot.PostingQueued || snapshot.EntriesQueued {
		return false
	}
	for _, line := range lines {
		if line.Closed {
			return false
		}
	}
	return true
}

// StepRevision operates on a frozen active revision. Newer user edits and a
// cancellation are desired state; they cannot change an in-flight payload.
func (p *SabyOrderRealizationProvider) StepRevision(ctx context.Context, command ordersale.RevisionCommand, progress ordersale.Progress) (ordersale.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result := ordersale.Result{Progress: progress, State: "queued"}
	var receipt sabyRealizationRevisionReceipt
	attention := func(message string) (ordersale.Result, error) {
		result.State, result.Message = "needs_attention", message
		if receipt.Version == 1 {
			result.Progress.RevisionNative, _ = json.Marshal(receipt)
		}
		return result, nil
	}
	queue := func(stage, phase, message string) (ordersale.Result, error) {
		receipt.Phase, receipt.Polls = phase, 0
		result.Progress.Stage = stage
		result.Progress.RevisionNative, _ = json.Marshal(receipt)
		result.Message = message
		return result, nil
	}
	wait := func(message string) (ordersale.Result, error) {
		if receipt.Polls < 10 {
			receipt.Polls++
		}
		result.RetryAfter, result.Message = 5*time.Second, message
		result.Progress.RevisionNative, _ = json.Marshal(receipt)
		if receipt.Polls >= 10 {
			result.State = "needs_attention"
			result.Message += "; после 10 проверок нужна ручная сверка"
		}
		return result, nil
	}
	if p == nil || !p.Client.Configured() || command.RevisionID <= 0 || command.CurrentRevisionID < 0 || command.RevisionID == command.CurrentRevisionID || command.Target.RevisionID != command.RevisionID || command.Current.RevisionID != 0 && command.Current.RevisionID != command.CurrentRevisionID ||
		!sabyRevisionSameIdentity(command.Origin, command.Current) || !sabyRevisionSameIdentity(command.Origin, command.Target) ||
		!command.Target.Event.CanPrepare || !sabyRealizationFingerprint.MatchString(command.Target.Event.Fingerprint) || !sabyRealizationFingerprint.MatchString(command.Current.Event.Fingerprint) {
		return attention("Не подтверждена неизменная привязка редакции реализации")
	}
	identity, err := sabyRealizationProviderIdentity(command.Origin, progress)
	if err != nil {
		return attention("Редакция ожидает точную привязку исходной реализации; новые документы не создаются")
	}
	currentPlan, currentErr := sabyRealizationProviderPlan(command.Current.Event.Lines)
	targetPlan, targetErr := sabyRealizationProviderPlan(command.Target.Event.Lines)
	if currentErr != nil || targetErr != nil {
		return attention("Не подтверждены товары, количества или цены редакции")
	}
	var origin sabyRealizationProviderReceipt
	if json.Unmarshal(progress.Native, &origin) != nil || origin.Version != 1 || origin.RequestHash != sabyRevisionHash(command.Origin) || !sabyRealizationProviderPreparedCreateMatches(origin.CreatePayload, command.Origin) {
		return attention("Исходное подтверждение документа повреждено; редакция не отправлялась")
	}
	if len(progress.RevisionNative) == 0 {
		if progress.Unknown || strings.HasPrefix(progress.Stage, "reconcile-") {
			return attention("Сначала требуется разрешить неизвестный результат исходной записи")
		}
		receipt = sabyRealizationRevisionReceipt{Version: 1, RevisionID: command.RevisionID, CurrentRevisionID: command.CurrentRevisionID, OriginHash: sabyRevisionHash(command.Origin), CurrentHash: sabyRevisionHash(command.Current), TargetHash: sabyRevisionHash(command.Target), Phase: "inspect"}
	} else if json.Unmarshal(progress.RevisionNative, &receipt) != nil || receipt.Version != 1 || receipt.RevisionID != command.RevisionID || receipt.CurrentRevisionID != command.CurrentRevisionID || receipt.OriginHash != sabyRevisionHash(command.Origin) || receipt.CurrentHash != sabyRevisionHash(command.Current) || receipt.TargetHash != sabyRevisionHash(command.Target) || receipt.Polls < 0 || receipt.Polls > 10 || receipt.LineIndex < 0 || receipt.LineIndex > len(targetPlan) {
		return attention("Сохранённая редакция не совпала с замороженным составом; запись остановлена")
	}
	snapshot, err := p.Client.ReadOrderRealizationWithLines(ctx, identity)
	if err != nil {
		return wait("Не удалось прочитать состояние редактируемой реализации")
	}
	if p.validateHeader(snapshot, command.Origin, origin.CreatePayload) != nil || snapshot.BuyerID != 0 && snapshot.BuyerID != command.Target.BuyerID {
		return attention("Реквизиты исходного документа изменились; редакция не отправлялась")
	}
	lines, err := p.Client.ReadOrderRealizationPricedLines(ctx, identity)
	if err != nil || !sabyRealizationProviderCancelSameReads(snapshot.Lines, lines) {
		return wait("Не удалось согласованно прочитать строки редактируемой реализации")
	}
	if snapshot.PostingErrors || snapshot.StaleEntries {
		return attention("В реализации ошибки проводок; изменение состава остановлено")
	}
	if snapshot.PostingQueued || snapshot.EntriesQueued {
		return wait("Ожидаем завершение предыдущей складской операции")
	}
	stage := progress.Stage
	if !strings.HasPrefix(stage, "revision-") {
		if len(progress.RevisionNative) != 0 {
			if stage == "verified" && receipt.TargetSawPosted {
				stage = "revision-verify-post"
			} else {
				return attention("Неизвестный этап сохранённой редакции; повторная запись запрещена")
			}
		} else {
			stage = "revision-inspect"
		}
	}
	if snapshot.BuyerID == 0 {
		if !sabyRevisionOpen(snapshot, lines, false) || len(lines) != 0 || len(progress.ActiveOwnedRowIDs) != 0 || len(progress.OwnedRowIDs) != 0 {
			return attention("Не подтверждён пустой собственный документ перед привязкой покупателя")
		}
		if progress.DesiredState == "cancelled" {
			receipt.UnpostVerified = true
			sabyRevisionUnpostProof(command, &result, snapshot, lines)
			return sabyRevisionCancelled(command, result, receipt, snapshot, lines)
		}
		if stage == "revision-verify-bind" || progress.Unknown {
			return wait("Проверяем неизвестный результат привязки покупателя без повторной записи")
		}
		if stage != "revision-bind" {
			return queue("revision-bind", "bind", "Подготовлена привязка покупателя исходного документа")
		}
		bind := map[string]any{"_type": "record", "s": []any{map[string]any{"n": "@Документ", "t": "Число целое"}, map[string]any{"n": "Лицо1", "t": "Связь"}}, "d": []any{identity.ID, command.Target.BuyerID}}
		receipt.LastReceipt, _, _ = p.Client.sabyOrderRealizationWriteRPCOnce(ctx, "РеалИсх.Записать", map[string]any{"Запись": bind})
		result.Progress.Unknown = true
		return queue("revision-verify-bind", "bind", "Результат привязки покупателя проверяется чтением")
	}
	if stage == "revision-bind" || stage == "revision-verify-bind" {
		result.Progress.Unknown = false
		return queue("revision-inspect", "inspect", "Покупатель подтверждён; проверяем состав перед редакцией")
	}
	if stage == "revision-inspect" {
		owned := progress.ActiveOwnedRowIDs
		if owned == nil {
			owned = progress.OwnedRowIDs
		}
		if !sabyRevisionExactIDs(owned, lines) || !sabyRealizationProviderRetainsLines(origin.KnownLines, lines) && command.Current.RevisionID == 0 {
			return attention("Не подтверждены точные собственные строки исходной редакции")
		}
		next, e := sabyRealizationProviderNext(currentPlan, lines)
		if e != nil {
			return attention("Фактический состав исходной редакции изменился вне заказа")
		}
		receipt.OriginalLines, receipt.KnownLines = lines, lines
		result.Progress.ActiveOwnedRowIDs = sabyRevisionIDs(lines)
		if snapshot.HeaderPosted && snapshot.ClosedOutgoingLines && next == len(currentPlan) {
			receipt.SawPosted = true
			result.Progress.Unknown = false
			return queue("revision-unpost", "unpost", "Подтверждён прежний состав; подготовлено снятие проведения того же документа")
		}
		if !sabyRevisionOpen(snapshot, lines, false) || progress.PostAttempted && !origin.PostingVerified {
			return wait("Не разрешена прежняя попытка проведения; новая редакция не отправляется")
		}
		receipt.UnpostVerified = true
		sabyRevisionUnpostProof(command, &result, snapshot, lines)
		return queue("revision-verify-delete", "delete-next", "Документ не проведён; проверяем собственные строки перед заменой")
	}
	// Unknown writes are reconciled using the unchanged receipt; no branch below
	// resets them to a fresh mutation merely because a target changed.
	if stage == "revision-unpost" || stage == "revision-verify-unpost" {
		if !sabyRevisionExactIDs(sabyRevisionIDs(receipt.OriginalLines), lines) || !sabyRealizationProviderRetainsLines(receipt.OriginalLines, lines) {
			return attention("Строки изменились до подтверждения снятия проведения")
		}
		if receipt.SawPosted && sabyRevisionOpen(snapshot, lines, true) {
			receipt.UnpostVerified, result.Progress.Unknown = true, false
			sabyRevisionUnpostProof(command, &result, snapshot, lines)
			return queue("revision-verify-delete", "delete-next", "Снятие проведения подтверждено; документ и история сохранены")
		}
		if stage != "revision-unpost" || progress.Unknown {
			return wait("Проверяем неизвестный результат снятия проведения без повторной записи")
		}
		cancelled, _ := p.Client.CancelOrderRealizationPosting(ctx, SabyOrderRealizationCancelRequest{Identity: identity, PublicUUID: command.Origin.OperationKey, OrganizationID: command.Current.OrganizationID, RegulationID: command.Current.RegulationID, BuyerID: command.Current.BuyerID, Lines: sabyRevisionPostLines(currentPlan), ExpectedRowIDs: sabyRevisionIDs(receipt.OriginalLines)})
		receipt.LastReceipt = cancelled.Receipt
		result.Progress.Unknown = true
		return queue("revision-verify-unpost", "unpost", "Снятие проведения отправлено; результат проверяется чтением")
	}
	if !receipt.UnpostVerified {
		return attention("Нет подтверждения отсутствия прежнего движения; изменение строк запрещено")
	}
	if receipt.TargetPostSent || stage == "revision-post" || stage == "revision-verify-post" || stage == "revision-cancel" || stage == "revision-verify-cancel" {
		return p.sabyRevisionPostStep(ctx, command, result, receipt, snapshot, lines, targetPlan, identity, stage)
	}
	if !sabyRevisionOpen(snapshot, lines, false) {
		return attention("Документ провели во время замены состава; запись остановлена")
	}
	if stage == "revision-delete" || stage == "revision-verify-delete" {
		return p.sabyRevisionDeleteStep(ctx, command, result, receipt, snapshot, lines, targetPlan, identity, stage)
	}
	if !sabyRevisionExactIDs(progress.ActiveOwnedRowIDs, lines) || !sabyRealizationProviderRetainsLines(receipt.KnownLines, lines) {
		// A positive save acknowledgement may appear later; it must never be
		// replaced by an equal group with a different row ID.
		if stage != "revision-verify-save" {
			return attention("Подтверждённые строки новой редакции изменились")
		}
	}
	if progress.DesiredState == "cancelled" && stage != "revision-verify-save" && stage != "revision-verify-prepare" {
		return sabyRevisionCancelled(command, result, receipt, snapshot, lines)
	}
	next, e := sabyRealizationProviderNext(targetPlan, lines)
	if e != nil {
		return attention("Новый состав содержит лишние или изменённые товары")
	}
	if stage == "revision-verify-save" {
		if receipt.LineIndex >= len(targetPlan) || !sabyRealizationProviderRetainsLines(receipt.KnownLines, lines) {
			return attention("Собственные строки изменились после отправки строки")
		}
		if receipt.SavedRowID <= 0 {
			return attention("СБИС не подтвердил ID сохранённой строки; совпадение состава не подтверждает владение, требуется ручная сверка")
		}
		if !sabyRealizationProviderAcknowledgedRow(receipt.SavedRowID, targetPlan[receipt.LineIndex], lines) || !sabyRealizationProviderHasLine(targetPlan[receipt.LineIndex], lines) {
			return wait("Ожидаем подтверждённую строку новой редакции; повторное добавление запрещено")
		}
		receipt.KnownLines, receipt.SavedRowID, receipt.Prepared = lines, 0, nil
		result.Progress.ActiveOwnedRowIDs = sabyRevisionIDs(lines)
		result.Progress.OwnedRowIDs = sabyRealizationProviderOwnIDs(result.Progress.OwnedRowIDs, sabyRevisionIDs(lines)...)
		result.Progress.Unknown = false
		if progress.DesiredState == "cancelled" {
			return sabyRevisionCancelled(command, result, receipt, snapshot, lines)
		}
		return queue("revision-verify-delete", "filled", "Строка новой редакции подтверждена")
	}
	if stage == "revision-verify-prepare" {
		if progress.DesiredState == "cancelled" {
			return sabyRevisionCancelled(command, result, receipt, snapshot, lines)
		}
		if receipt.Prepared == nil || progress.Unknown {
			return attention("Ответ подготовки строки потерян; повторная подготовка запрещена")
		}
		if next != receipt.LineIndex {
			return attention("Состав изменился после подготовки строки")
		}
		return queue("revision-save", "save", "Подготовленная строка проверена перед единственной записью")
	}
	if receipt.LineIndex != next || next >= len(targetPlan) {
		return attention("Неверный номер строки новой редакции")
	}
	fill := SabyOrderRealizationFillRequest{Identity: identity, OrganizationID: command.Target.OrganizationID, RegulationID: command.Target.RegulationID, BuyerID: command.Target.BuyerID, Lines: targetPlan, Next: targetPlan[next]}
	switch stage {
	case "revision-prepare":
		prepared, err := p.Client.PrepareOrderRealizationRow(ctx, fill)
		receipt.LastReceipt = prepared.Receipt
		if err != nil {
			if prepared.Outcome == "unknown" {
				result.Progress.Unknown = true
				return queue("revision-verify-prepare", "prepare", "Проверяем неизвестный результат подготовки строки")
			}
			return attention("Не удалось безопасно подготовить строку новой редакции")
		}
		receipt.Prepared = prepared.Prepared
		return queue("revision-verify-prepare", "prepared", "Шаблон строки сохранён; проверяем состав")
	case "revision-save":
		if receipt.Prepared == nil || progress.Unknown {
			return attention("Нет подтверждённого шаблона для единственной записи строки")
		}
		saved, err := p.Client.SavePreparedOrderRealizationRow(ctx, fill, *receipt.Prepared)
		receipt.LastReceipt = saved.Receipt
		if err == nil {
			receipt.SavedRowID = saved.RowID
			result.Progress.ActiveOwnedRowIDs = sabyRealizationProviderOwnIDs(result.Progress.ActiveOwnedRowIDs, saved.RowID)
			result.Progress.OwnedRowIDs = sabyRealizationProviderOwnIDs(result.Progress.OwnedRowIDs, saved.RowID)
		} else if saved.Outcome != "unknown" {
			return attention("Сохранение строки не прошло проверку; повторная запись запрещена")
		}
		result.Progress.Unknown = err != nil
		return queue("revision-verify-save", "saved", "Проверяем единственное сохранение строки новой редакции")
	}
	return attention("Неизвестный этап редакции; внешняя запись не выполнялась")
}

// Retired rows remain in the immutable receipt; only the active ID set shrinks.
func sabyRevisionRemove(lines []SabyOrderRealizationPricedLine, id int64) []SabyOrderRealizationPricedLine {
	out := make([]SabyOrderRealizationPricedLine, 0, len(lines))
	for _, line := range lines {
		if line.ID != id {
			out = append(out, line)
		}
	}
	return out
}

func sabyRevisionCancelled(command ordersale.RevisionCommand, result ordersale.Result, receipt sabyRealizationRevisionReceipt, snapshot SabyOrderRealizationSnapshot, lines []SabyOrderRealizationPricedLine) (ordersale.Result, error) {
	if !receipt.UnpostVerified || !sabyRevisionOpen(snapshot, lines, false) || !sabyRevisionExactIDs(result.Progress.ActiveOwnedRowIDs, lines) {
		return result, errors.New("Не подтверждено отсутствие движения изменённого документа")
	}
	proof := sabyRealizationProviderCancellationDocument(command.Target, result.Progress, snapshot, lines)
	proof.RevisionID = command.RevisionID
	kind := "revision_unposted"
	if receipt.TargetPostSent || result.Progress.RevisionPostAttempted {
		if !receipt.TargetSawPosted || !sabyRevisionOpen(snapshot, lines, true) {
			return result, errors.New("Не разрешена попытка проведения новой редакции")
		}
		kind = "unposted_full"
	}
	result.State, result.Message = "cancelled", "Отмена подтверждена без продолжения изменения состава"
	result.Progress.Stage, result.Progress.Unknown = "cancelled", false
	result.Progress.RevisionNative, _ = json.Marshal(receipt)
	result.CancellationVerified = true
	result.CancellationProof, _ = json.Marshal(ordersale.CancellationProof{Kind: kind, Document: &proof})
	return result, nil
}

func sabyRevisionUnpostProof(command ordersale.RevisionCommand, result *ordersale.Result, snapshot SabyOrderRealizationSnapshot, lines []SabyOrderRealizationPricedLine) {
	proof := sabyRealizationProviderCancellationDocument(command.Current, result.Progress, snapshot, lines)
	proof.RevisionID = command.RevisionID
	result.RevisionUnpostVerified = true
	result.RevisionUnpostProof, _ = json.Marshal(proof)
	result.Progress.RevisionUnpostVerified = true
}

func (p *SabyOrderRealizationProvider) sabyRevisionDeleteStep(ctx context.Context, command ordersale.RevisionCommand, result ordersale.Result, receipt sabyRealizationRevisionReceipt, snapshot SabyOrderRealizationSnapshot, lines []SabyOrderRealizationPricedLine, target []SabyOrderRealizationFillLine, identity SabyOrderRealizationIdentity, stage string) (ordersale.Result, error) {
	done := func(state, message string) (ordersale.Result, error) {
		result.State, result.Message = state, message
		result.Progress.RevisionNative, _ = json.Marshal(receipt)
		return result, nil
	}
	wait := func(message string) (ordersale.Result, error) {
		if receipt.Polls < 10 {
			receipt.Polls++
		}
		result.RetryAfter = 5 * time.Second
		if receipt.Polls >= 10 {
			return done("needs_attention", message+"; требуется ручная сверка")
		}
		return done("queued", message)
	}
	if receipt.DeleteRowID > 0 {
		if stage == "revision-delete" && !result.Progress.Unknown && result.Progress.DesiredState == "cancelled" {
			if !sabyRevisionExactIDs(sabyRevisionIDs(receipt.KnownLines), lines) || !sabyRealizationProviderRetainsLines(receipt.KnownLines, lines) {
				return done("needs_attention", "Состав изменился до отмены неподтверждённого удаления")
			}
			receipt.DeleteRowID = 0
			return sabyRevisionCancelled(command, result, receipt, snapshot, lines)
		}
		remaining := sabyRevisionRemove(receipt.KnownLines, receipt.DeleteRowID)
		if len(remaining) != len(receipt.KnownLines)-1 {
			return done("needs_attention", "Повреждено подтверждение удаляемой строки")
		}
		if sabyRevisionExactIDs(sabyRevisionIDs(remaining), lines) && sabyRealizationProviderRetainsLines(remaining, lines) {
			for _, line := range receipt.KnownLines {
				if line.ID == receipt.DeleteRowID {
					receipt.RetiredLines = append(receipt.RetiredLines, line)
				}
			}
			result.Progress.RetiredRowIDs = sabyRealizationProviderOwnIDs(result.Progress.RetiredRowIDs, receipt.DeleteRowID)
			result.Progress.ActiveOwnedRowIDs = sabyRevisionIDs(lines)
			receipt.KnownLines, receipt.DeleteRowID, receipt.Polls = lines, 0, 0
			result.Progress.Unknown = false
			proof := sabyRealizationProviderCancellationDocument(command.Target, result.Progress, snapshot, lines)
			proof.RevisionID = command.RevisionID
			readback, _ := json.Marshal(proof)
			result.RevisionRetirementProof, _ = json.Marshal(ordersale.RevisionRetirementProof{Version: 1, RevisionID: command.RevisionID, OriginHash: receipt.OriginHash, CurrentHash: receipt.CurrentHash, TargetHash: receipt.TargetHash, DocumentID: result.Progress.DocumentID, DocumentUUID: result.Progress.DocumentUUID, RetiredRowIDs: append([]int64{}, result.Progress.RetiredRowIDs...), RemainingRowIDs: append([]int64{}, result.Progress.ActiveOwnedRowIDs...), Readback: readback})
			result.Progress.Stage, receipt.Phase = "revision-verify-delete", "delete-next"
			return done("queued", "Отсутствие удалённой строки подтверждено чтением того же документа")
		}
		if !sabyRevisionExactIDs(sabyRevisionIDs(receipt.KnownLines), lines) || !sabyRealizationProviderRetainsLines(receipt.KnownLines, lines) {
			return done("needs_attention", "Во время удаления изменились другие собственные строки")
		}
		if stage != "revision-delete" || result.Progress.Unknown {
			return wait("Проверяем неизвестный результат удаления; повторное удаление запрещено")
		}
		deleted, _ := p.Client.DeleteOrderRealizationRow(ctx, SabyOrderRealizationDeleteRequest{Identity: identity, PublicUUID: command.Origin.OperationKey, OrganizationID: command.Target.OrganizationID, RegulationID: command.Target.RegulationID, BuyerID: command.Target.BuyerID, ExpectedLines: receipt.KnownLines, RowID: receipt.DeleteRowID})
		receipt.LastReceipt = deleted.Receipt
		result.Progress.Unknown, result.Progress.Stage = true, "revision-verify-delete"
		return done("queued", "Удаление собственной строки отправлено; проверяем отсутствие без повторной записи")
	}
	if !sabyRevisionExactIDs(result.Progress.ActiveOwnedRowIDs, lines) || !sabyRevisionExactIDs(sabyRevisionIDs(receipt.KnownLines), lines) || !sabyRealizationProviderRetainsLines(receipt.KnownLines, lines) {
		return done("needs_attention", "Состав изменился перед следующей операцией редакции")
	}
	if result.Progress.DesiredState == "cancelled" {
		return sabyRevisionCancelled(command, result, receipt, snapshot, lines)
	}
	// Once all origin rows were retired, only fresh target rows may be present.
	if len(receipt.RetiredLines) < len(receipt.OriginalLines) {
		if len(lines) == 0 {
			return done("needs_attention", "Исходные строки исчезли без сохранённого подтверждения удаления")
		}
		receipt.DeleteRowID, receipt.Polls = lines[0].ID, 0
		result.Progress.Stage, receipt.Phase = "revision-delete", "delete"
		return done("queued", "Подготовлено единственное удаление точной собственной строки")
	}
	next, err := sabyRealizationProviderNext(target, lines)
	if err != nil {
		return done("needs_attention", "После замены обнаружен посторонний состав")
	}
	receipt.Polls, receipt.LineIndex = 0, next
	result.Progress.Unknown = false
	if next == len(target) {
		result.Progress.Stage, receipt.Phase = "revision-post", "post"
		return done("queued", "Новая редакция проверена; подготовлено проведение того же документа")
	}
	result.Progress.Stage, receipt.Phase = "revision-prepare", "prepare"
	return done("queued", "Подготовлена следующая строка нового состава")
}

func (p *SabyOrderRealizationProvider) sabyRevisionPostStep(ctx context.Context, command ordersale.RevisionCommand, result ordersale.Result, receipt sabyRealizationRevisionReceipt, snapshot SabyOrderRealizationSnapshot, lines []SabyOrderRealizationPricedLine, target []SabyOrderRealizationFillLine, identity SabyOrderRealizationIdentity, stage string) (ordersale.Result, error) {
	done := func(state, message string) (ordersale.Result, error) {
		result.State, result.Message = state, message
		result.Progress.RevisionNative, _ = json.Marshal(receipt)
		return result, nil
	}
	wait := func(message string) (ordersale.Result, error) {
		if receipt.Polls < 10 {
			receipt.Polls++
		}
		result.RetryAfter = 5 * time.Second
		if receipt.Polls >= 10 {
			return done("needs_attention", message+"; требуется ручная сверка")
		}
		return done("queued", message)
	}
	next, err := sabyRealizationProviderNext(target, lines)
	if err != nil || next != len(target) || !sabyRevisionExactIDs(result.Progress.ActiveOwnedRowIDs, lines) || !sabyRevisionExactIDs(sabyRevisionIDs(receipt.KnownLines), lines) || !sabyRealizationProviderRetainsLines(receipt.KnownLines, lines) {
		return done("needs_attention", "Полный состав и ID новой редакции не совпали; повторная запись запрещена")
	}
	posted := snapshot.HeaderPosted && snapshot.ClosedOutgoingLines
	if posted {
		receipt.TargetSawPosted = true
	}
	if result.Progress.DesiredState == "cancelled" {
		if !receipt.TargetPostSent && !result.Progress.RevisionPostAttempted {
			return sabyRevisionCancelled(command, result, receipt, snapshot, lines)
		}
		if receipt.TargetSawPosted && sabyRevisionOpen(snapshot, lines, true) {
			return sabyRevisionCancelled(command, result, receipt, snapshot, lines)
		}
		if !posted {
			return wait("Ожидаем разрешение проведения новой редакции перед отменой")
		}
		if stage == "revision-cancel" && !result.Progress.Unknown {
			cancelled, _ := p.Client.CancelOrderRealizationPosting(ctx, SabyOrderRealizationCancelRequest{Identity: identity, PublicUUID: command.Origin.OperationKey, OrganizationID: command.Target.OrganizationID, RegulationID: command.Target.RegulationID, BuyerID: command.Target.BuyerID, Lines: sabyRevisionPostLines(target), ExpectedRowIDs: result.Progress.ActiveOwnedRowIDs})
			receipt.LastReceipt = cancelled.Receipt
			result.Progress.Stage, result.Progress.Unknown = "revision-verify-cancel", true
			return done("queued", "Отмена новой редакции проверяется без повторной записи")
		}
		if stage == "revision-verify-cancel" {
			return wait("Проверяем результат отмены новой редакции")
		}
		result.Progress.Stage, result.Progress.Unknown = "revision-cancel", false
		receipt.Polls = 0
		return done("queued", "Проведение новой редакции подтверждено; подготовлена единственная отмена")
	}
	if posted {
		proof := sabyRealizationProviderCancellationDocument(command.Target, result.Progress, snapshot, lines)
		proof.RevisionID = command.RevisionID
		result.RevisionVerified, result.StockVerified = true, true
		result.RevisionProof, _ = json.Marshal(proof)
		result.StockProof = result.RevisionProof
		result.Progress.Stage, result.Progress.Unknown = "verified", false
		return done("posted", "Новый состав проведён в исходном документе; движение подтверждено")
	}
	if stage != "revision-post" || result.Progress.Unknown || receipt.TargetPostSent {
		return wait("Проверяем проведение нового состава без повторного запроса")
	}
	if !sabyRevisionOpen(snapshot, lines, false) {
		return done("needs_attention", "Складское состояние не допускает проведение нового состава")
	}
	postedResult, _ := p.Client.PostOrderRealization(ctx, SabyOrderRealizationPostRequest{Identity: identity, OrganizationID: command.Target.OrganizationID, RegulationID: command.Target.RegulationID, BuyerID: command.Target.BuyerID, Lines: sabyRevisionPostLines(target)})
	receipt.LastReceipt, receipt.TargetPostSent = postedResult.Receipt, true
	result.Progress.Unknown, result.Progress.Stage = true, "revision-verify-post"
	return done("queued", "Проведение нового состава отправлено; ожидаем независимое подтверждение")
}
