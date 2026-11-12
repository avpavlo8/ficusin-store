package chat

import (
	"context"
	"errors"
	"testing"
)

func TestReferenceValidation(t *testing.T) {
	for _, r := range []*ReferenceInput{{Kind: "product", ID: "12"}, {Kind: "order", ID: "90"}} {
		if _, _, err := Validate(&Input{ClientID: "reference-client-0001", Reference: r}); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []*ReferenceInput{{Kind: "other", ID: "1"}, {Kind: "order", ID: "-2"}, {Kind: "product", ID: "http://evil"}, {Kind: "product", ID: "001"}, {Kind: "order", ID: "9223372036854775808"}} {
		if _, _, err := Validate(&Input{ClientID: "reference-client-0002", Reference: r}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %#v: %v", r, err)
		}
	}
}
func TestChatReferenceOwnershipAndReplacement(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `INSERT INTO customers VALUES(3,'Другой покупатель');INSERT INTO products VALUES(101,9001,'Товар для чата','published'),(102,9002,'Архивный товар','archived');INSERT INTO orders(id,customer_id,order_number,status)VALUES(201,1,'0000-201','new'),(202,3,'0000-202','new')`); err != nil {
		t.Fatal(err)
	}
	buyer := Identity{CustomerID: 1}
	threadID, err := s.Send(ctx, buyer, 0, 0, Input{ClientID: "reference-product-client-0001", Reference: &ReferenceInput{Kind: "product", ID: "9001"}})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.Read(ctx, threadID, 0)
	if err != nil || len(thread.Messages) != 1 {
		t.Fatal(thread, err)
	}
	ref := thread.Messages[0].Reference
	if ref == nil || ref.Label != "Товар для чата" || ref.ID != "9001" || !ref.Available {
		t.Fatalf("bad product snapshot: %#v", ref)
	}
	opts, err := s.ReferenceOptions(ctx, buyer, 0, 0, false, "order", "")
	if err != nil || len(opts.Items) != 1 || opts.Items[0].ID != "201" {
		t.Fatal(opts, err)
	}
	opts, err = s.ReferenceOptions(ctx, Identity{}, 2, threadID, true, "order", "")
	if err != nil || len(opts.Items) != 1 || opts.Items[0].ID != "201" {
		t.Fatal("staff leaked other customer's order", opts, err)
	}
	if _, err = s.ReferenceOptions(ctx, Identity{}, 2, threadID, false, "order", ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("staff order permission bypass", err)
	}
	opts, err = s.ReferenceOptions(ctx, Identity{}, 0, 0, false, "order", "")
	if err != nil || !opts.LoginRequired || len(opts.Items) != 0 {
		t.Fatal(opts, err)
	}
	for _, trial := range []struct {
		who   Identity
		staff int64
		can   bool
		ref   ReferenceInput
	}{
		{buyer, 0, false, ReferenceInput{Kind: "order", ID: "202"}},
		{Identity{}, 2, true, ReferenceInput{Kind: "order", ID: "202"}},
		{Identity{}, 2, false, ReferenceInput{Kind: "order", ID: "201"}},
		{buyer, 0, false, ReferenceInput{Kind: "product", ID: "9002"}},
	} {
		_, err = s.Send(ctx, trial.who, trial.staff, threadID, Input{ClientID: "invalid-reference-client-0001", Reference: &trial.ref, CanReferenceOrders: trial.can})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("unauthorized reference sent: %#v %v", trial, err)
		}
	}
	if _, err = s.Send(ctx, Identity{}, 2, threadID, Input{ClientID: "staff-reference-client-0001", Reference: &ReferenceInput{Kind: "order", ID: "201"}, CanReferenceOrders: true}); err != nil {
		t.Fatal(err)
	}
	thread, err = s.ReadAfter(ctx, threadID, thread.Messages[0].ID)
	if err != nil || len(thread.Messages) != 1 || thread.Messages[0].Reference.Number != "0000-201" {
		t.Fatal(thread, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE products SET status='archived' WHERE product_code=9001`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Send(ctx, buyer, 0, 0, Input{ClientID: "reference-product-client-0001", Reference: &ReferenceInput{Kind: "product", ID: "9001"}}); err != nil {
		t.Fatalf("retry lost a sent reference receipt: %v", err)
	}
	thread, err = s.Read(ctx, threadID, 0)
	if err != nil || thread.Messages[0].Reference.Available {
		t.Fatal("archived product still linked", err)
	}
	if len(thread.Messages) != 2 {
		t.Fatal("rejected references persisted messages")
	}
}
