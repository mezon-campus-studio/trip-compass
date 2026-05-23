# Phân việc — Backend Dev (Go + Planner-AI)

Công việc cần làm để backend + planner-ai đáp ứng đủ các flow trong [docs/integration/](.). Sắp xếp theo priority P0 → P3.

---

## P0 — Khoá chặn frontend integration

### BE-1. Broadcast WS event từ service layer sau khi ghi DB
**Context**: Hiện `ws.Hub` chỉ forward message **client → client**. Khi user A sửa activity qua REST, user B không nhận được event realtime nếu không tự publish.

**Làm**:
- Inject `*ws.Hub` vào `ActivityService` và `ItineraryService` (hoặc qua interface `Broadcaster`).
- Sau khi commit DB thành công trong `Create`, `Update`, `Delete`, `Reorder`, `Publish`:
  ```go
  hub.BroadcastToRoom(itineraryID, serialize(Message{
      Type: "activity.updated",
      Payload: jsonOf(activity),
      Sender: &SenderInfo{UserID: userID, FullName: userName},
  }), nil)
  ```
- Đồng thời publish lên Redis (`redisPubSub.Publish`) để cross-server.
- Sender exclusion: truyền `userID` xuống service → hub skip client có cùng user_id (hoặc dùng `client.ID`).

**DoD**: 2 tab mở cùng itinerary, sửa ở tab A → tab B tự cập nhật không cần reload.

**File**: `backend/internal/services/{activity,itinerary}.go`, `backend/internal/ws/hub.go`.

---

### BE-2. Mở rộng planner-ai `create_travel_plan` nhận preferences địa điểm
**Context**: UX place-picker (user chọn "muốn đi / bỏ qua") hiện phải nhúng vào text prompt → LLM-dependent.

**Làm**:
- Thêm tham số vào tool `create_travel_plan` (`planner-ai/app/tools/create_plan.py`):
  ```python
  must_include_place_ids: Optional[list[str]] = None
  exclude_place_ids: Optional[list[str]] = None
  ```
- Truyền xuống `node_schedule`:
  - `must_include`: bắt buộc reserve slot cho các place này trước khi fill phần còn lại. Nếu không nhét đủ → thêm violation `MUST_INCLUDE_SKIPPED`.
  - `exclude`: filter `retrieved["places"]` trước khi schedule.
- Mở rộng `PlanRequest` schema (`planner-ai/app/schemas.py`) thêm 2 field tương ứng → expose qua `POST /plan`.
- Backend Go `planner.GenerateRequest` thêm `MustIncludePlaceIDs []string`, `ExcludePlaceIDs []string` và proxy xuống planner-ai.

**DoD**: POST `/plan` với `must_include_place_ids=[uuid1]` → uuid1 phải xuất hiện trong `days[].slots[].place.id`.

---

### BE-3. Endpoint atomic `POST /itineraries/from-plan`
**Context**: Frontend đang phải gọi `POST /itineraries` + N × `POST /activities`. Nếu lỗi giữa chừng sẽ để lại itinerary rỗng.

**Làm**: Thêm handler:
```
POST /api/v1/itineraries/from-plan (protected)
body: {
  title, destination, start_date, end_date, guest_count, tags, budget_category,
  plan: GenerateResponse  // payload y nguyên từ /planner/generate
}
→ 201 Itinerary (kèm activities đã tạo)
```

Service chạy trong 1 transaction: tạo itinerary → loop `plan.days[].slots[]` tạo activity (bỏ buffer) → commit.

**DoD**: Frontend chỉ cần 1 request để lưu plan AI thành itinerary.

---

### BE-4. Search/filter mở rộng cho `/places`
**Context**: Frontend place-picker và `/places` cần search theo tên + lọc theo `tags`, `min_rating`.

**Làm**: Mở rộng `PlaceService.List` và handler:
```
GET /api/v1/places
  ?destination=&category=&q=&tags=a,b&min_rating=4&must_visit=true
  &page=&limit=
```
- `q`: ILIKE `%q%` trên `name` và `name_en`.
- `tags`: split CSV → `tags && ARRAY[...]::text[]`.
- `min_rating`: `rating >= ?`.
- `must_visit`: boolean.

**DoD**: `GET /places?destination=đà nẵng&q=bà&tags=beach` trả đúng kết quả.

---

## P1 — Hoàn thiện tính năng chính

### BE-5. Collaborators (mời cộng tác itinerary)
**Context**: Đã có model `Collaborator` + WS check quyền, thiếu API.

**Làm**: Thêm routes (`handlers/collaborator.go`):
```
POST   /itineraries/:id/collaborators        body:{email, role}     (chủ itinerary mời)
GET    /itineraries/:id/collaborators                               (list + status)
PATCH  /collaborators/:id                    body:{status:ACCEPTED|DECLINED}  (người được mời)
DELETE /collaborators/:id                                            (chủ hoặc chính user)
GET    /user/invitations                                              (list pending)
```
Email thông báo (tái dùng `services/email.go`).

---

### BE-6. Like / favorite itinerary
```
POST   /itineraries/:id/like     → tăng likes_count, bản ghi user_itinerary_like
DELETE /itineraries/:id/like
```
Mở rộng model `Itinerary` thêm `LikesCount int` và bảng pivot.

---

### BE-7. Forgot password
```
POST /auth/forgot-password   body:{email}   (luôn 200)
POST /auth/reset-password    body:{token, new_password}
```
Reuse flow verify token + email template.

---

### BE-8. Admin role middleware
**Context**: Hiện `/admin/*` chỉ check JWT, chưa check role.

**Làm**:
- Thêm field `Role string` ("USER" | "ADMIN") vào `User` model + migration.
- Middleware `RequireAdmin` đọc claim hoặc query DB.
- Apply vào toàn bộ group `admin` + `POST /knowledge-base/seed`.

---

### BE-9. Forward SSE chat qua Go backend (tuỳ chọn)
**Context**: Hiện frontend gọi thẳng planner-ai — lộ URL và không auth. Nếu muốn ẩn planner-ai + gắn rate-limit theo user:
```
POST /api/v1/ai/chat/stream (protected)  → proxy SSE tới planner-ai
POST /api/v1/ai/chat                     → proxy non-stream
GET  /api/v1/ai/sessions/...             → proxy
```
**Không làm** nếu chấp nhận frontend gọi trực tiếp.

---

## P2 — DX & chất lượng

- **BE-10**. Logger structured (slog) + request ID middleware, log latency per route.
- **BE-11**. OpenAPI/Swagger spec tự sinh (`swaggo/swag`) — giúp FE sync contract.
- **BE-12**. Unit test `ActivityService.Reorder` (race condition trên unique index).
- **BE-13**. Integration test end-to-end flow "AI plan → save → edit → publish" (dùng testcontainers Postgres + Redis).
- **BE-14**. Rate-limit theo user (hiện theo IP) cho `/planner/generate` khi đã login.
- **BE-15**. Soft-delete cho `Itinerary` (thêm `deleted_at`) — tránh mất dữ liệu user.
- **BE-16**. Retention chat history planner-ai: cron xoá session > 30 ngày không active.

---

## P3 — Tối ưu

- **BE-17**. Full-text search places (Postgres `tsvector` + GIN index) thay ILIKE.
- **BE-18**. Index thêm cho `itineraries(status, destination)` cho `/explore`.
- **BE-19**. Caching layer Redis cho `GET /places/:id`, `GET /combos/:id` (TTL 10 phút, invalidate khi PATCH).
- **BE-20**. Planner-ai: cache `get_places` / `get_food_venues` trong Redis (key = destination+limit) TTL 1h.
- **BE-21**. Planner-ai: streaming intermediate plan updates (hiện chỉ bắn token text, không bắn plan JSON progressively).

---

## Cross-cutting checklist trước prod

- [ ] `ALLOWED_ORIGINS` set đúng domain production
- [ ] JWT_SECRET ≥ 32 bytes random
- [ ] HTTPS bắt buộc (redirect http→https ở reverse proxy)
- [ ] WebSocket upgrader `CheckOrigin` whitelist domain (hiện `return true`)
- [ ] Rate-limit toàn bộ auth endpoints (brute-force email/password)
- [ ] Validate file upload avatar (size, mimetype) — hiện chưa có endpoint upload, có thể dùng S3 presigned
- [ ] Backup Postgres hàng ngày
- [ ] Health-check `/health` gắn liveness/readiness cho k8s/ECS
