package auth

import "context"

// RoleAdmin is the role string carried by JWT claims and API keys for administrators.
const RoleAdmin = "admin"

// Actor is the authenticated caller as the data layer sees it.
//
// Its zero value is "nobody": UserID 0 matches no instance row and Admin is false, so a
// forgotten initialisation denies access instead of granting it. That property is the
// whole reason this is a struct rather than a sentinel user ID — with a bare int64 an
// actor and a plain userID would be interchangeable at every call site, and the natural
// defensive fix for a nil user (`var uid int64; if u != nil { uid = u.ID }`) would
// silently become full admin access.
type Actor struct {
	UserID int64
	Admin  bool
}

// ActorFrom builds the Actor for a request context. ok is false when the request carries
// no authenticated user; the caller must reject rather than continue with a zero Actor.
func ActorFrom(ctx context.Context) (Actor, bool) {
	u := UserFromContext(ctx)
	if u == nil {
		return Actor{}, false
	}
	return Actor{UserID: u.ID, Admin: u.Role == RoleAdmin}, true
}
