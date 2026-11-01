package procurement_test

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	app "github.com/amirfaisalz/procure-flow/internal/application/procurement"
	"github.com/amirfaisalz/procure-flow/internal/domain"
	"github.com/amirfaisalz/procure-flow/internal/domain/audit"
	domainproc "github.com/amirfaisalz/procure-flow/internal/domain/procurement"
	"github.com/amirfaisalz/procure-flow/internal/infrastructure/memory"
)

func ctxAs(subject string, roles ...string) context.Context {
	return app.WithActor(context.Background(), app.Actor{
		Subject: subject,
		Name:    subject,
		Roles:   roles,
	})
}

func adminCtx() context.Context {
	return ctxAs("user_admin", app.RoleAdmin)
}

func requesterCtx(subject string) context.Context {
	return ctxAs(subject, app.RoleRequester)
}

type fakeAuditRepo struct {
	events    []audit.Event
	appendErr error
}

func (f *fakeAuditRepo) Append(_ context.Context, event audit.Event) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	event.ID = int64(len(f.events) + 1)
	f.events = append(f.events, event)
	return nil
}

func (f *fakeAuditRepo) List(_ context.Context, filter audit.ListFilter) (audit.ListResult, error) {
	out := make([]audit.Event, 0, len(f.events))
	for _, event := range f.events {
		if event.EntityType == filter.EntityType && event.EntityID == filter.EntityID {
			out = append(out, event)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })

	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = audit.DefaultPageSize
	}
	total := len(out)
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return audit.ListResult{
		Events:   append([]audit.Event(nil), out[start:end]...),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

type fakeUnitOfWork struct {
	calls int
}

func (f *fakeUnitOfWork) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	return fn(ctx)
}

func TestServiceFailsClosedWithoutActor(t *testing.T) {
	repo := &fakeRepo{has: true, item: domainproc.Procurement{ID: "prc_1", Status: domainproc.StatusDraft, Version: 1}}
	svc := newService(repo)
	ctx := context.Background()

	calls := []struct {
		name string
		call func() error
	}{
		{"Create", func() error { _, err := svc.Create(ctx, validInput()); return err }},
		{"Get", func() error { _, err := svc.Get(ctx, "prc_1"); return err }},
		{"List", func() error { _, err := svc.List(ctx, domainproc.ListFilter{}); return err }},
		{"Submit", func() error { _, err := svc.Submit(ctx, "prc_1"); return err }},
		{"Approve", func() error { _, err := svc.Approve(ctx, "prc_1"); return err }},
		{"Reject", func() error { _, err := svc.Reject(ctx, "prc_1", "no budget"); return err }},
		{"Cancel", func() error { _, err := svc.Cancel(ctx, "prc_1"); return err }},
		{"Audit", func() error { _, err := svc.Audit(ctx, "prc_1", audit.ListFilter{}); return err }},
	}

	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			if err := call.call(); !errors.Is(err, domain.ErrForbidden) {
				t.Fatalf("%s() error = %v, want ErrForbidden", call.name, err)
			}
		})
	}
}

func TestServiceCreateStampsRequester(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(repo)

	input := validInput()
	input.Requester = ""

	got, err := svc.Create(requesterCtx("user_amir"), input)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got.RequesterID != "user_amir" {
		t.Errorf("RequesterID = %q, want user_amir", got.RequesterID)
	}
	if got.Requester != "user_amir" {
		t.Errorf("Requester = %q, want the actor name", got.Requester)
	}
}

func TestServiceCreateRequiresRequesterRole(t *testing.T) {
	repo := &fakeRepo{}
	svc := newService(repo)

	_, err := svc.Create(ctxAs("user_viewer", app.RoleViewer), validInput())
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Create() error = %v, want ErrForbidden", err)
	}
	if repo.createCalled {
		t.Error("repository Create was called for a forbidden actor")
	}
}

func TestServiceReadAllowedForViewer(t *testing.T) {
	repo := &fakeRepo{
		has:        true,
		item:       domainproc.Procurement{ID: "prc_1"},
		listResult: domainproc.ListResult{Items: []domainproc.Procurement{}, Page: 1, PageSize: 20},
	}
	svc := newService(repo)
	ctx := ctxAs("user_viewer", app.RoleViewer)

	if _, err := svc.Get(ctx, "prc_1"); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if _, err := svc.List(ctx, domainproc.ListFilter{}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
}

func TestServiceSubmitOwnership(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		owner   string
		wantErr bool
	}{
		{name: "owner requester", ctx: requesterCtx("user_amir"), owner: "user_amir"},
		{name: "other requester", ctx: requesterCtx("user_budi"), owner: "user_amir", wantErr: true},
		{name: "admin override", ctx: adminCtx(), owner: "user_amir"},
		{name: "viewer", ctx: ctxAs("user_viewer", app.RoleViewer), owner: "user_viewer", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{has: true, item: domainproc.Procurement{
				ID: "prc_1", Status: domainproc.StatusDraft, Version: 1, RequesterID: tt.owner,
			}}
			svc := newService(repo)

			_, err := svc.Submit(tt.ctx, "prc_1")
			if tt.wantErr {
				if !errors.Is(err, domain.ErrForbidden) {
					t.Fatalf("Submit() error = %v, want ErrForbidden", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Submit() error = %v", err)
			}
		})
	}
}

func TestServiceApproveDepartmentScope(t *testing.T) {
	tests := []struct {
		name       string
		actor      app.Actor
		department string
		wantErr    bool
	}{
		{
			name:       "matching department",
			actor:      app.Actor{Subject: "user_fin", Name: "Fin", Roles: []string{app.RoleApprover}, Department: "Finance"},
			department: "Finance",
		},
		{
			name:       "department case-insensitive",
			actor:      app.Actor{Subject: "user_fin", Name: "Fin", Roles: []string{app.RoleApprover}, Department: "finance"},
			department: "Finance",
		},
		{
			name:       "wrong department",
			actor:      app.Actor{Subject: "user_fin", Name: "Fin", Roles: []string{app.RoleApprover}, Department: "IT"},
			department: "Finance",
			wantErr:    true,
		},
		{
			name:       "approver without department",
			actor:      app.Actor{Subject: "user_fin", Name: "Fin", Roles: []string{app.RoleApprover}},
			department: "Finance",
			wantErr:    true,
		},
		{
			name:       "requester cannot approve",
			actor:      app.Actor{Subject: "user_amir", Name: "Amir", Roles: []string{app.RoleRequester}, Department: "Finance"},
			department: "Finance",
			wantErr:    true,
		},
		{
			name:       "admin override",
			actor:      app.Actor{Subject: "user_admin", Name: "Admin", Roles: []string{app.RoleAdmin}},
			department: "Finance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{has: true, item: domainproc.Procurement{
				ID: "prc_1", Status: domainproc.StatusSubmitted, Version: 2, Department: tt.department,
			}}
			svc := newService(repo)
			ctx := app.WithActor(context.Background(), tt.actor)

			_, err := svc.Approve(ctx, "prc_1")
			if tt.wantErr {
				if !errors.Is(err, domain.ErrForbidden) {
					t.Fatalf("Approve() error = %v, want ErrForbidden", err)
				}
				if repo.updateCalled {
					t.Error("repository Update was called for a forbidden actor")
				}
				return
			}
			if err != nil {
				t.Fatalf("Approve() error = %v", err)
			}
		})
	}
}

func TestServiceRejectDepartmentScope(t *testing.T) {
	repo := &fakeRepo{has: true, item: domainproc.Procurement{
		ID: "prc_1", Status: domainproc.StatusSubmitted, Version: 2, Department: "Finance",
	}}
	svc := newService(repo)

	_, err := svc.Reject(requesterCtx("user_amir"), "prc_1", "budget exceeded")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Reject() error = %v, want ErrForbidden", err)
	}

	ctx := app.WithActor(context.Background(), app.Actor{
		Subject: "user_fin", Name: "Fin", Roles: []string{app.RoleApprover}, Department: "Finance",
	})
	if _, err := svc.Reject(ctx, "prc_1", "budget exceeded"); err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
}

func TestServiceCancelOwnership(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		owner   string
		wantErr bool
	}{
		{name: "owner", ctx: requesterCtx("user_amir"), owner: "user_amir"},
		{name: "other requester", ctx: requesterCtx("user_budi"), owner: "user_amir", wantErr: true},
		{name: "admin", ctx: adminCtx(), owner: "user_amir"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{has: true, item: domainproc.Procurement{
				ID: "prc_1", Status: domainproc.StatusDraft, Version: 1, RequesterID: tt.owner,
			}}
			svc := newService(repo)

			_, err := svc.Cancel(tt.ctx, "prc_1")
			if tt.wantErr {
				if !errors.Is(err, domain.ErrForbidden) {
					t.Fatalf("Cancel() error = %v, want ErrForbidden", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Cancel() error = %v", err)
			}
		})
	}
}

func TestServiceAuditRecordsLifecycle(t *testing.T) {
	procurements := memory.NewProcurementRepository()
	auditRepo := memory.NewAuditRepository()
	uow := &fakeUnitOfWork{}

	svc := app.NewService(
		procurements,
		fakeIDs{id: "prc_1", number: "PR-202610-0001"},
		func() time.Time { return fixedNow },
		app.WithAudit(auditRepo, uow),
	)

	owner := requesterCtx("user_amir")
	created, err := svc.Create(owner, validInput())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := svc.Submit(owner, created.ID); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if _, err := svc.Approve(adminCtx(), created.ID); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}

	result, err := svc.Audit(owner, created.ID, audit.ListFilter{})
	if err != nil {
		t.Fatalf("Audit() error = %v", err)
	}
	if len(result.Events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(result.Events))
	}
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}

	wantActions := []string{app.ActionApproved, app.ActionSubmitted, app.ActionCreated}
	for i, want := range wantActions {
		if result.Events[i].Action != want {
			t.Errorf("events[%d].Action = %q, want %q", i, result.Events[i].Action, want)
		}
	}
	if result.Events[0].ActorID != "user_admin" {
		t.Errorf("approval actor = %q, want user_admin", result.Events[0].ActorID)
	}
	if result.Events[2].FromStatus != "" || result.Events[2].ToStatus != string(domainproc.StatusDraft) {
		t.Errorf("created event statuses = %q -> %q", result.Events[2].FromStatus, result.Events[2].ToStatus)
	}
	if uow.calls == 0 {
		t.Error("unit of work was never used")
	}
}

func TestServiceAuditAppendFailurePropagates(t *testing.T) {
	repo := &fakeRepo{}
	auditRepo := &fakeAuditRepo{appendErr: errors.New("audit down")}
	svc := app.NewService(
		repo,
		fakeIDs{id: "prc_1", number: "PR-202610-0001"},
		func() time.Time { return fixedNow },
		app.WithAudit(auditRepo, &fakeUnitOfWork{}),
	)

	_, err := svc.Create(requesterCtx("user_amir"), validInput())
	if err == nil || err.Error() != "audit down" {
		t.Fatalf("Create() error = %v, want audit down", err)
	}
}

func TestServiceAuditPagination(t *testing.T) {
	repo := &fakeRepo{has: true, item: domainproc.Procurement{ID: "prc_1"}}
	auditRepo := &fakeAuditRepo{}
	for i := 0; i < 5; i++ {
		auditRepo.events = append(auditRepo.events, audit.Event{
			EntityType: audit.EntityProcurement,
			EntityID:   "prc_1",
			Action:     "procurement.action",
		})
	}
	svc := app.NewService(repo, fakeIDs{}, nil, app.WithAudit(auditRepo, &fakeUnitOfWork{}))

	result, err := svc.Audit(adminCtx(), "prc_1", audit.ListFilter{Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("Audit() error = %v", err)
	}
	if result.Total != 5 || result.Page != 2 || result.PageSize != 2 {
		t.Errorf("result meta = %+v, want total=5 page=2 size=2", result)
	}
	if len(result.Events) != 2 {
		t.Errorf("len(events) = %d, want 2", len(result.Events))
	}
}

func TestServiceAuditRejectsInvalidPagination(t *testing.T) {
	repo := &fakeRepo{has: true, item: domainproc.Procurement{ID: "prc_1"}}
	svc := app.NewService(repo, fakeIDs{}, nil, app.WithAudit(&fakeAuditRepo{}, &fakeUnitOfWork{}))

	tests := []struct {
		name   string
		filter audit.ListFilter
		field  string
	}{
		{name: "negative page", filter: audit.ListFilter{Page: -1}, field: "page"},
		{name: "page size too large", filter: audit.ListFilter{PageSize: audit.MaxPageSize + 1}, field: "page_size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Audit(adminCtx(), "prc_1", tt.filter)
			requireFieldError(t, err, tt.field)
		})
	}
}

func TestServiceAuditNotFound(t *testing.T) {
	repo := &fakeRepo{}
	svc := app.NewService(repo, fakeIDs{}, nil, app.WithAudit(&fakeAuditRepo{}, &fakeUnitOfWork{}))

	_, err := svc.Audit(adminCtx(), "prc_missing", audit.ListFilter{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Audit() error = %v, want ErrNotFound", err)
	}
}
