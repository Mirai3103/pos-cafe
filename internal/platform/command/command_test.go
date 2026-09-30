package command

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIDToLockKey(t *testing.T) {
	id := uuid.MustParse("00000000-0000-0001-ffff-ffffffffffff")
	assert.Equal(t, int64(1), IDToLockKey(id), "only the first eight bytes form the key")

	negative := uuid.MustParse("ffffffff-ffff-ffff-0000-000000000000")
	assert.Equal(t, int64(-1), IDToLockKey(negative), "the bytes are bit-cast, not range-checked")
}

func TestRequestLockKey(t *testing.T) {
	staff, request := uuid.New(), uuid.New()
	assert.Equal(t, IDToLockKey(staff)^IDToLockKey(request), RequestLockKey(staff, request))
	assert.Equal(t, RequestLockKey(staff, request), RequestLockKey(request, staff))
}

func TestFingerprintHash(t *testing.T) {
	got, err := FingerprintHash(struct {
		Name string `json:"name"`
	}{Name: "Ban 1"})
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(`{"name":"Ban 1"}`))), got)

	_, err = FingerprintHash(make(chan int))
	require.ErrorContains(t, err, "marshal mutation fingerprint")
}

func TestVerifyCapabilities(t *testing.T) {
	require.NoError(t, VerifyCapabilities(nil, nil))
	require.NoError(t, VerifyCapabilities([]string{"a", "b"}, []string{"b", "c", "a"}))

	err := VerifyCapabilities([]string{"a", "z"}, []string{"a"})
	require.ErrorIs(t, err, ErrForbidden)
	assert.EqualError(t, err, `forbidden: missing capability "z"`)
}

func TestPolicyIsSecurityDenial(t *testing.T) {
	errWrongPIN := errors.New("wrong pin")
	p := Policy{SecurityDenials: []error{errWrongPIN}}

	assert.True(t, p.IsSecurityDenial(fmt.Errorf("%w: session locked", ErrUnauthorized)))
	assert.True(t, p.IsSecurityDenial(fmt.Errorf("%w: identity disabled", ErrForbidden)))
	assert.True(t, p.IsSecurityDenial(fmt.Errorf("%w: pin rejected", errWrongPIN)))
	assert.False(t, p.IsSecurityDenial(fmt.Errorf("load staff: %w", sql.ErrConnDone)))
	assert.False(t, p.IsSecurityDenial(nil))
	assert.False(t, Policy{}.IsSecurityDenial(errWrongPIN), "only the policy's own sentinels count")
}

func TestPolicyApprovalDenied(t *testing.T) {
	assert.Same(t, ErrForbidden, Policy{}.approvalDenied())

	unavailable := errors.New("manager approval unavailable")
	assert.Same(t, unavailable, Policy{ApprovalDenied: unavailable}.approvalDenied())
}

func TestDenialAttribution(t *testing.T) {
	actor := Actor{StaffID: uuid.New(), SessionID: uuid.New()}
	confirmed := Authority{SessionID: actor.SessionID, StaffIdentityID: actor.StaffID}
	var missing Authority // the reload found no row

	cases := []struct {
		name        string
		attribution Attribution
		authority   Authority
		wantActor   bool
		wantSession bool
	}{
		{"always, confirmed", AttributeActorAlways, confirmed, true, true},
		{"always, missing", AttributeActorAlways, missing, true, false},
		{"if confirmed, confirmed", AttributeActorIfConfirmed, confirmed, true, true},
		{"if confirmed, missing", AttributeActorIfConfirmed, missing, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actorID, sessionID := Policy{Attribution: tc.attribution}.denialAttribution(actor, tc.authority)
			assert.Equal(t, tc.wantActor, actorID.Valid)
			assert.Equal(t, tc.wantSession, sessionID.Valid)
			if actorID.Valid {
				assert.Equal(t, actor.StaffID, actorID.UUID)
			}
			if sessionID.Valid {
				assert.Equal(t, actor.SessionID, sessionID.UUID)
			}
		})
	}
}

type describingStore struct{ IdempotencyKeys }

func (describingStore) DescribeConflict(stored StoredRequest, operation, requestHash string) string {
	return fmt.Sprintf("%s/%s vs %s/%s", stored.Operation, stored.RequestHash, operation, requestHash)
}

func TestConflictDetail(t *testing.T) {
	stored := StoredRequest{Operation: "op.a", RequestHash: "h1"}

	plain := NewRunner(nil, nil, Policy{DenialEventType: "x.authorization_denied"})
	assert.Equal(t, "request_id reused for a different change", plain.conflictDetail(stored, "op.b", "h2"))

	described := NewRunner(nil, nil, Policy{DenialEventType: "x.authorization_denied", Idempotency: describingStore{}})
	assert.Equal(t, "op.a/h1 vs op.b/h2", described.conflictDetail(stored, "op.b", "h2"))
}

func TestNewRunner(t *testing.T) {
	assert.Panics(t, func() { NewRunner(nil, nil, Policy{}) }, "a policy without a denial event type is a wiring bug")

	r := NewRunner(nil, nil, Policy{DenialEventType: "x.authorization_denied"})
	assert.IsType(t, IdempotencyKeys{}, r.policy.Idempotency, "idempotency_keys is the default store")
}

func TestDenialLogMessage(t *testing.T) {
	assert.Equal(t, "sales authorization denied",
		NewRunner(nil, nil, Policy{DenialEventType: "x", LogScope: "sales"}).denialLogMessage())
	assert.Equal(t, "authorization denied",
		NewRunner(nil, nil, Policy{DenialEventType: "x"}).denialLogMessage())
}
