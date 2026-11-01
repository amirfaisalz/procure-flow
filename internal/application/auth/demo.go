package auth

// DemoPassword is the development password used when SEED_PASSWORD is unset.
// Seeding refuses to use it in production.
const DemoPassword = "demo-password-123"

// DemoAccount describes a seedable account. The set covers every role so the
// full authorization matrix can be exercised from a fresh database.
type DemoAccount struct {
	Email      string
	Name       string
	Roles      []string
	Department string
}

// DemoAccounts returns the accounts created by cmd/seed and the compose seed
// service. Emails use .local so they can never collide with a real domain.
func DemoAccounts() []DemoAccount {
	return []DemoAccount{
		{Email: "admin@procure.local", Name: "Local Admin", Roles: []string{"admin"}},
		{Email: "approver@procure.local", Name: "Finance Approver", Roles: []string{"approver"}, Department: "Engineering"},
		{Email: "requester@procure.local", Name: "Amir Requester", Roles: []string{"requester"}, Department: "Engineering"},
		{Email: "viewer@procure.local", Name: "Read Only", Roles: []string{"viewer"}},
	}
}
