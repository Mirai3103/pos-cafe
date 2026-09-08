# Architecture Decision Records (ADR) - POS Cafe Backend

Tài liệu ghi nhận các quyết định kiến trúc và thiết kế kỹ thuật trong quá trình chuyển đổi từ TypeScript sang Golang.

---

## ADR-001: Multi-Role Model cho Nhân viên (`staff_operational_roles`)
- **Ngày quyết định:** 2026-09-09
- **Trạng thái:** Accepted
- **Bối cảnh:** Hệ thống cũ cho phép một nhân viên có nhiều vai trò cùng lúc (ví dụ: `MANAGER` kiêm `CASHIER`, hoặc `CASHIER` kiêm `BARISTA`).
- **Quyết định:** Giữ nguyên mô hình Multi-role với bảng quan hệ `staff_operational_roles(staff_identity_id, role)`.
- **Hệ quả:** Tương thích 100% với Frontend React và logic phân quyền nghiệp vụ POS hiện tại.

---

## ADR-002: Thuật toán băm mã PIN bằng `bcrypt`
- **Ngày quyết định:** 2026-09-09
- **Trạng thái:** Accepted
- **Bối cảnh:** Hệ thống cũ dùng `argon2id` từ thư viện Node.js. Cần chọn giải pháp tối ưu cho Go backend.
- **Quyết định:** Sử dụng thư viện chuẩn `golang.org/x/crypto/bcrypt` với cost tiêu chuẩn để băm và kiểm tra mã PIN nhân viên (4–8 chữ số).
- **Hệ quả:** Tối ưu hóa hiệu năng và dung lượng bộ nhớ trên phần cứng POS cấu hình thấp (Celeron, 2–4GB RAM).

---

## ADR-003: Cơ chế xác thực Token Hybrid (Bearer Header + Cookie)
- **Ngày quyết định:** 2026-09-09
- **Trạng thái:** Accepted
- **Bối cảnh:** Cần phục vụ linh hoạt cho cả Web SPA client, Desktop Terminal và các ứng dụng phần cứng nhúng.
- **Quyết định:** Hỗ trợ cơ chế Hybrid:
  - Khi đăng nhập/mở khóa thành công, API trả về `token` trong JSON response body và đồng thời set HTTP-only cookie.
  - Middleware `RequireAuth` ưu tiên đọc token từ header `Authorization: Bearer <token>`, nếu không có sẽ fallback đọc từ Cookie `staff_session_token`.
- **Hệ quả:** Linh hoạt tối đa cho mọi loại client, dễ test qua Postman/curl và Swagger UI.

---

## ADR-004: Giới hạn kích thước cột tường minh (`VARCHAR(n)`)
- **Ngày quyết định:** 2026-09-09
- **Trạng thái:** Accepted
- **Bối cảnh:** Mặc dù PostgreSQL lưu trữ `TEXT` và `VARCHAR` tương đương về RAM/Disk, nhưng việc không giới hạn độ dài ở tầng database có nguy cơ bị phình dữ liệu nếu client gửi payload quá lớn.
- **Quyết định:** Sử dụng các kiểu dữ liệu có giới hạn độ dài tường minh cho các bảng:
  - `display_name`: `VARCHAR(120)`
  - `login_code`: `VARCHAR(24)`
  - `pin_hash`: `VARCHAR(72)` (vừa vặn với chuỗi 60 ký tự của bcrypt)
  - `token_hash`: `VARCHAR(64)` (chuỗi hex SHA-256)
  - `role`, `state`, `active_workspace`: `VARCHAR(20)`
- **Hệ quả:** Tăng cường tính toàn vẹn dữ liệu (Data Integrity) ngay từ tầng cơ sở dữ liệu.

---

## ADR-005: Hợp nhất các bảng Idempotency thành bảng duy nhất (`idempotency_keys`)
- **Ngày quyết định:** 2026-09-09
- **Trạng thái:** Accepted
- **Bối cảnh:** Hệ thống TypeScript cũ tạo mỗi use case một bảng (`staff_identity_creation_requests`, `staff_identity_enabled_state_requests`, `staff_identity_role_replacement_requests`, `staff_identity_pin_reset_requests`), dẫn đến anti-pattern phình schema (Schema Bloat) và lãng phí tài nguyên.
- **Quyết định:**
  - Thay thế toàn bộ 4 bảng cũ bằng **1 bảng duy nhất** theo pattern chuẩn Stripe / IETF:
    ```sql
    CREATE TABLE idempotency_keys (
        key UUID NOT NULL,                       -- request_id từ frontend
        actor_id UUID,                           -- ID nhân viên thao tác
        action VARCHAR(50) NOT NULL,             -- 'staff.create', 'staff.set_enabled', etc.
        request_hash VARCHAR(64) NOT NULL,       -- SHA-256 payload để phát hiện sửa đổi
        response_code INT NOT NULL,              -- HTTP status code (200, 201...)
        response_body JSONB NOT NULL,            -- Kết quả JSON để replay khi retry
        created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
        PRIMARY KEY (actor_id, key)
    );
    ```
  - Bảng này sẽ được tái sử dụng chung cho toàn bộ các slice sau này như `shift`, `sales`, `payments` mà không cần tạo thêm bảng nào khác.
- **Hệ quả:**
  - Database schema cực kỳ gọn gàng, loại bỏ hoàn toàn mã lặp.
  - Hỗ trợ replay response tức thì khi mạng chập chờn hoặc máy POS bấm đúp.
  - Dễ dàng thiết lập cronjob tự động dọn rác (clean up các record cũ hơn 24h).
