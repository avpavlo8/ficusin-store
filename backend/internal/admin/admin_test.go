package admin

import "testing"

func TestRolePermissions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		role       string
		permission string
		want       bool
	}{
		{name: "owner can edit roles", role: RoleOwner, permission: PermissionRolesEdit, want: true},
		{name: "owner can edit integrations", role: RoleOwner, permission: PermissionIntegrationsEdit, want: true},
		{name: "administrator can edit procurement", role: RoleAdministrator, permission: PermissionProcurementEdit, want: true},
		{name: "administrator cannot edit roles", role: RoleAdministrator, permission: PermissionRolesEdit, want: false},
		{name: "administrator cannot access finance", role: RoleAdministrator, permission: PermissionFinanceRead, want: false},
		{name: "administrator cannot edit integrations", role: RoleAdministrator, permission: PermissionIntegrationsEdit, want: false},
		{name: "administrator cannot delete", role: RoleAdministrator, permission: PermissionDelete, want: false},
		{name: "manager can edit orders", role: RoleManager, permission: PermissionOrdersEdit, want: true},
		{name: "manager can edit products", role: RoleManager, permission: PermissionProductsEdit, want: true},
		{name: "manager cannot sync products", role: RoleManager, permission: PermissionProductsSync, want: false},
		{name: "manager cannot edit customers", role: RoleManager, permission: PermissionCustomersEdit, want: false},
		{name: "manager cannot edit discounts", role: RoleManager, permission: PermissionDiscountsEdit, want: false},
		{name: "unknown role has no access", role: "unknown", permission: PermissionDashboard, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Can(test.role, test.permission); got != test.want {
				t.Fatalf("Can(%q, %q) = %v, want %v", test.role, test.permission, got, test.want)
			}
		})
	}
}

func TestStaffRolesAreAssignable(t *testing.T) {
	t.Parallel()

	if !AssignableRole("") || !AssignableRole(RoleManager) || !AssignableRole(RoleAdministrator) {
		t.Fatal("staff roles and role removal must be assignable")
	}
	if AssignableRole(RoleOwner) {
		t.Fatal("owner role must never be assignable through the application")
	}
}
