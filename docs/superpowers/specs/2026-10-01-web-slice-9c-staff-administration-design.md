# Design Specification: Web Slice 9c — Staff Administration (`./web` + `internal/auth`, Phase 11 UI)

- **Author:** Claude & Team
- **Date:** 2026-10-01
- **Status:** Draft, pending review
- **Phase:** Phase 11, UI half — see [`docs/backlog/phase-11-clients-over-lan-and-single-binary.md`](../../backlog/phase-11-clients-over-lan-and-single-binary.md)
- **Predecessors:**
  - [`2026-09-21-web-frontend-slice-sequence-design.md`](2026-09-21-web-frontend-slice-sequence-design.md) (slice sequence & definition of done)
  - [`2026-09-28-web-slice-9a-availability-design.md`](2026-09-28-web-slice-9a-availability-design.md) (splits slice 9; tabbed settings, ADR-056)
  - [`2026-09-30-web-slice-9b-catalog-structure-design.md`](2026-09-30-web-slice-9b-catalog-structure-design.md) (table-and-modal admin pattern)
- **Visual authority:** [`design-system/pos-cafe/DESIGN.md`](../../../design-system/pos-cafe/DESIGN.md). The prototype has **no** staff page; this tab follows the 9b views' table and modal styling.
- **Domain authority:** [`CONTEXT.md`](../../../CONTEXT.md) (Staff Identity, Operational Role, Capability, Staff Access Session), `internal/auth/` (Phase 1), ADR-048

---

## 1. Purpose & Scope

Slice 9c gives a Manager a screen for the Staff Identities they today manage by
script: list them, add one, change name, login code and roles, reset a PIN, and
disable or re-enable an account. It is the last sub-slice of slice 9.

### 1.1 In Scope

- A new Go command, `PATCH /staff/{id}`: change display name, login code, and roles
  in one atomic command (section 2, ADR-063).
- The last-enabled-Manager invariant moves into one shared function used by the new
  command and `PUT /staff/{id}/roles`.
- A new settings tab "Nhân viên" at `/settings/staff`, guarded by `staff.administer`
  (section 3).
- A staff table with search and a "show disabled" filter, a create/edit modal, a
  reset-PIN modal, and a disable/enable confirmation (section 4).
- Self-protection on the signed-in Manager's own row (section 5).

### 1.2 Out of Scope (Recorded Cuts)

| Element | Why it is cut |
| --- | --- |
| Audit Events for staff commands | No `internal/auth` command writes an Audit Event today, which ADR-048 requires. Recorded as backlog item **BA-6** in [`backend-alignment.md`](../../backlog/backend-alignment.md); not fixed here. |
| Deleting a Staff Identity | No endpoint, and a Staff Identity is the retained actor of past business actions (`CONTEXT.md`). Disabling is the removal. |
| Seeing who is signed in, revoking one session | No endpoint. Disable or reset PIN already revokes every session. |
| Self-service PIN change | A different job (any role, own PIN). Not requested. |
| Backend refusal of self-targeted actions | The web blocks them (section 5); the backend keeps allowing them while another Manager exists. |

---

## 2. Backend: update staff command

### 2.1 Contract

`PATCH /staff/{id}`, in the existing `/staff` group (requires the `MANAGER` role).

```json
{
  "request_id": "uuid",
  "display_name": "Nguyễn Văn Minh",
  "login_code": "MINH",
  "roles": ["CASHIER", "BARISTA"],
  "manager_pin": "1234"
}
```

Validation mirrors `CreateStaffRequest`: `display_name` 1–120, `login_code` 1–24,
`roles` at least one of `MANAGER CASHIER BARISTA`, `manager_pin` 4–8 digits. All
three editable fields are required: the web always sends the whole form, so there is
no partial-update ambiguity.

Response: `200` with `StaffDetailResponse`.

### 2.2 Behavior

Same shape as `staff_replace_roles.go`:

1. Verify `manager_pin` against the **acting** Staff Identity (constant-time path
   when the actor is missing).
2. `ExecuteWithIdempotency` with operation `staff.update` and payload
   `{target_id, body}`.
3. Inside the transaction: take `EnabledManagerInvariantLockID`, load the target
   (`404` if absent), load current roles, run the shared invariant check, then
   `UpdateStaffProfile` (new sqlc query: sets `display_name` and
   `login_code = upper(btrim(...))`, returns the row), clear and re-add roles.
4. A unique violation on `login_code` (`23505`) returns `409` "mã đăng nhập đã được
   sử dụng", as create does.

Changing the login code does not revoke sessions: a Staff Access Session belongs to
the Staff Identity, not the code.

### 2.3 Shared invariant

```go
// ensureManagerRemains returns ErrManagerInvariant when an enabled Manager would
// stop being one and no other enabled Manager exists. Caller holds the invariant lock.
func ensureManagerRemains(ctx context.Context, qtx *sqlc.Queries, targetEnabled bool, currentRoles, nextRoles []string) error
```

`staff_replace_roles.go` and `staff_update.go` call it. `staff_set_enabled.go` keeps
its own check, which asks a different question (disabling, not role removal).

### 2.4 `PUT /staff/{id}/roles` stays

It is kept for API compatibility; the web no longer calls it.

---

## 3. Route, tab, and guard

- `web/src/routes/_app/settings/staff.tsx`, `beforeLoad: requireCapability("staff.administer")`.
- `features/settings/lib/tabs.ts`: add `{ to: "/settings/staff", label: "Nhân viên",
  icon: Users, capability: "staff.administer" }` after the catalog tab. It joins
  `SETTINGS_CAPABILITIES`, so the `/settings` layout guard admits it (ADR-056).
- `SettingsLayout` counter for the tab: the number of enabled staff.

### 3.1 Code layout

```
web/src/features/staff/
  api/use-staff.ts              list query + four mutations
  components/staff-view.tsx     toolbar + table
  components/staff-table.tsx    rows, role badges, row actions
  components/staff-form-modal.tsx   create and edit
  components/reset-pin-modal.tsx
  components/toggle-enabled-dialog.tsx
  lib/staff.ts                  filter/search, form validation, self rules
```

---

## 4. Views

### 4.1 Table

Toolbar: "+ Thêm nhân viên", a search box (matches display name or login code,
case- and diacritic-insensitive), and a "Hiện tài khoản đã khóa" checkbox, off by
default. Sort: enabled first, then display name.

Columns: **Tên** (with a "Bạn" badge on the signed-in row), **Mã**, **Vai trò**
(badges: Quản lý, Thu ngân, Pha chế), **Trạng thái** (Hoạt động / Đã khóa), actions:
"Sửa", "Đặt lại PIN", and "Khóa" or "Mở khóa".

Empty state after filtering: "Không có nhân viên phù hợp".

### 4.2 Create / edit modal

Fields: display name, login code (shown and sent upper-case), roles as three
checkboxes. Create adds a PIN and a PIN confirmation (4–8 digits) and an "Kích hoạt
ngay" checkbox, on by default.

- Create → `POST /staff`. Edit → `PATCH /staff/{id}`. Edit with no change disables
  "Lưu".
- Client validation matches the server rules in 2.1 and `CreateStaffRequest`; PIN
  and confirmation must match.
- A `409` on the login code shows under the login code field and keeps the modal open.

### 4.3 Reset PIN modal

New PIN and confirmation (4–8 digits, must match). Copy states that the staff member
is signed out on every device. → `POST /staff/{id}/reset-pin`.

### 4.4 Disable / enable

A confirmation dialog. Disabling states that the staff member is signed out
everywhere. → `PATCH /staff/{id}/enabled` with `expected_enabled` = the row's
current value. A `409` stale state shows the server's message; the list is refetched (4.5).

### 4.5 Commands and errors

Every command gets a fresh `request_id` per intent (slice sequence §4.3) and a
Manager PIN through `ManagerApprovalDialog` (section 6). After every command,
successful or not, invalidate the staff list so a stale row is corrected. Errors
show inline in the open modal or dialog with the server's message; the last-Manager
`409` is reachable only when two Managers act concurrently.

---

## 5. The signed-in Manager's own row

Identified by `staffId` from the session store.

- "Đặt lại PIN" and "Khóa" are hidden: both revoke every session, including the
  one in use. Another Manager does it.
- In the edit modal, the "Quản lý" checkbox is checked and disabled: removing it
  would drop the user out of this tab mid-edit.
- Name and login code stay editable. After a successful self-edit, refetch
  `/auth/session` and `applyServerState`, so the header name and the stored login
  code stay current.

These rules live in `lib/staff.ts` as pure functions so they are unit-tested.

---

## 6. Manager PIN

The staff commands verify the PIN of the **acting** staff member; they take no
approver login code. `ManagerApprovalDialog` already pre-fills and locks the login
code field when the signed-in user holds `MANAGER`, which every user of this tab
does, so the dialog is reused unchanged.

---

## 7. Decisions and Documentation

- **ADR-063**: *Staff profile and roles change in one command.* `PATCH /staff/{id}`
  replaces display name, login code, and roles atomically. It differs from ADR-062's
  planned sequence because all three fields share one capability and one PIN rule,
  and a partial save could leave a role change applied without the rename or the
  reverse, which the last-Manager invariant makes likely to fail half-way. Rejected:
  a name/code-only endpoint run with the 9b plan runner.
- `backend-alignment.md`: add **BA-6**, Audit Events for staff administration.
- `ROADMAP.md`: link this spec; mark 9c done after UAT.

---

## 8. Testing

Go (per repo conventions, integration tests on the Postgres template):

- `PATCH /staff/{id}`: success updates name, upper-cased code, and roles; duplicate
  login code → `409`; removing `MANAGER` from the last enabled Manager → `409`
  invariant; wrong PIN → `403`; unknown id → `404`; same `request_id` replays the
  first result.
- `PUT /staff/{id}/roles` tests pass unchanged after the invariant extraction.

Web (`bun test`, pure logic and components, no end-to-end):

- `staff.test.ts`: search (diacritics, code), disabled filter, sort; form
  validation; self rules (hidden actions, locked Manager checkbox).
- `staff-table.test.tsx`: the "Bạn" row shows no reset/disable actions.

---

## 9. UAT Gate

Performed by the operator on the running app. Implementation stops here; completion
is not self-certified.

1. As a Manager, open "Nhân viên". A Cashier or Barista does not see the tab.
2. Create a Barista with a PIN. Sign in as them on another device.
3. Edit them: rename, change login code, add Cashier. Sign in again with the new code.
4. Reset their PIN; the other device is signed out; the new PIN works.
5. Disable them; the other device is signed out; sign-in is refused. Re-enable.
6. Own row: no reset/disable actions; the Manager checkbox is locked; renaming
   yourself updates the header.
7. Create a second Manager; remove their Manager role, then disable them. Both
   succeed. (The last-Manager refusal cannot be reached through this screen alone,
   since the actor is always an enabled Manager and cannot target themselves; it is
   covered by the Go tests and surfaces only on a race between two Managers.)
8. Create with an existing login code: the error shows on the field.
