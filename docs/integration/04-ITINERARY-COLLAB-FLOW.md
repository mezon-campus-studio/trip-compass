# 04 — Itinerary CRUD + Realtime cộng tác

## 1. Vòng đời một itinerary

```
                     [DRAFT] ───PATCH /publish───► [PUBLISHED]
   POST /itineraries     │                                  │
   (rỗng hoặc từ AI plan)│                                  │ GET /itineraries/:id/public
                         │                                  │ (không cần login)
   PATCH /itineraries/:id│
   POST /activities      │  POST /itineraries/:id/clone
   PATCH /activities/:id │  ────────────────────────────►  Bản clone của user khác
   PATCH /activities/    │
            reorder      │
   DELETE ...            │
                         ▼
                     DELETE /itineraries/:id
```

## 2. Tạo itinerary (3 đường vào)

| Đường | Trang | API |
|---|---|---|
| Tự nhập | `/itinerary/new` (wizard 2 bước) | `POST /itineraries` |
| Từ AI plan | `/ai-planner` → "Lưu thành lịch trình" | `POST /itineraries` + N×`POST /activities` (xem [03](03-AI-PLANNER-FLOW.md#4)) |
| Clone | `/explore` hoặc `/itinerary/[id]/public` → nút "Lưu về tài khoản" | `POST /itineraries/:id/clone` |

## 3. Trang chi tiết `/itinerary/[id]`

```
GET /itineraries/:id
  → Itinerary + activities[]
Render:
  - Hero: cover, title, destination, dates, status badge, view_count, rating
  - Sidebar trái: tabs ngày 1, 2, 3...
  - Main: list activity của ngày được chọn
  - Sticky right: budget recap (sum estimated_cost), actions (Edit, Clone, Publish, Share)
Action:
  - "Chỉnh sửa" → /itinerary/[id]/edit (chỉ owner)
  - "Nhân bản"  → POST /itineraries/:id/clone → redirect bản clone
  - "Xuất bản"  → PATCH /itineraries/:id/publish (chỉ owner)
  - "Chia sẻ"   → copy link /itinerary/[id]/public (chỉ active khi PUBLISHED)
```

## 4. Trang edit `/itinerary/[id]/edit`

### 4.1 Layout

```
┌──────────────────────────────────────────────────────────────────┐
│  Header: title (inline edit) | dates | budget | "Lưu" "Xuất bản"  │
├──────────┬───────────────────────────────────────────────────────┤
│ Day tabs │  Day 1 — 2026-05-01                                    │
│  • Day 1 │  ┌──────────────────────────────────────────────┐    │
│  • Day 2 │  │ 08:00-09:30  Cầu Vàng        2 km · 100k    ⋮│    │
│  • Day 3 │  │ 09:30-11:00  ăn trưa Mì Quảng              ⋮│    │
│  + thêm  │  │ 11:00-12:00  buffer (di chuyển)            ⋮│    │
│          │  │ ...                                          │    │
│          │  │ ➕ Thêm activity                              │    │
│          │  └──────────────────────────────────────────────┘    │
│          │  Tổng ngày: 1.250.000đ                                 │
├──────────┴───────────────────────────────────────────────────────┤
│  Avatar người đang cùng edit  · • 🟢 Bình  • 🟢 Mai                │
└──────────────────────────────────────────────────────────────────┘
```

### 4.2 Drag & drop

- Dùng `@dnd-kit/core` + `@dnd-kit/sortable` (đã có trong package.json).
- Mỗi activity có handle drag.
- Drop trong cùng ngày → đổi `order_index`.
- Drop sang ngày khác → đổi `day_number` + `order_index`.
- Sau khi thả → gom toàn bộ activity bị ảnh hưởng (cả 2 ngày), gửi 1 lần:

```ts
await apiFetch("/activities/reorder", {
  method: "PATCH",
  body: { items: updated.map((a, i) => ({ id: a.id, day_number: a.day_number, order_index: i })) },
});
```

> Backend dùng unique constraint `(itinerary_id, day_number, order_index)` — phải gửi reorder cho **tất cả** activity bị ảnh hưởng trong giao dịch (handler service sẽ chạy trong transaction).

### 4.3 Inline edit

Form bên phải khi click 1 activity (slide-in panel hoặc dialog):
- Search place: autocomplete `GET /places?q=&destination=` (cần thêm filter `q` ở backend nếu chưa có; hiện hỗ trợ `destination` + `category`).
- Đổi giờ, ghi chú, chi phí.
- Save → `PATCH /activities/:id`.
- Xoá → `DELETE /activities/:id`.

### 4.4 Autosave header (title/budget/dates)

Debounce 800ms từ khi user dừng gõ → `PATCH /itineraries/:id`.

---

## 5. WebSocket realtime

### 5.1 Kết nối

```ts
const url = `${NEXT_PUBLIC_WS_URL}/itinerary/${id}?token=${encodeURIComponent(jwt)}`;
const ws = new WebSocket(url);
```

Server kiểm tra:
- JWT hợp lệ (claims.sub)
- User là owner HOẶC collaborator status=ACCEPTED
- Nếu sai → đóng kết nối kèm 401/403 trước upgrade.

### 5.2 Protocol

Mọi message JSON:

```jsonc
{
  "type": "activity.created" | "activity.updated" | "activity.deleted" |
          "activity.reordered" | "itinerary.updated" |
          "presence.join" | "presence.leave" | "cursor" | "error",
  "payload": { ... },
  "sender": { "user_id": "...", "full_name": "Bình" }
}
```

Hub broadcast cho **tất cả client trong cùng room**, **trừ sender**. Sender đã có optimistic update local nên không cần echo.

### 5.3 Mapping event → action client

| Event | Payload mẫu | Client xử lý |
|---|---|---|
| `activity.created` | `{ activity: Activity }` | thêm vào state ngày tương ứng |
| `activity.updated` | `{ activity: Activity }` | replace by id |
| `activity.deleted` | `{ activity_id: string }` | remove by id |
| `activity.reordered` | `{ items: [{id,day_number,order_index}] }` | re-sort state |
| `itinerary.updated` | `{ itinerary: Itinerary }` | merge metadata |
| `presence.join` | `{ user_id, full_name }` | thêm avatar vào danh sách online |
| `presence.leave` | `{ user_id }` | bỏ khỏi danh sách |
| `cursor` (tuỳ chọn) | `{ user_id, activity_id, field }` | hiện caret màu của user khác |
| `error` | `{ message }` | toast |

> **Quan trọng**: hub hiện tại chỉ chuyển tiếp message **thô** giữa client. Nó **không** tự bắn event khi REST API ghi DB. Để các client khác thấy thay đổi:
>
> - **Phương án 1 (đơn giản, đang khả thi)**: Client thực hiện CRUD qua REST → sau khi 200 OK, **tự** publish 1 message qua WS để các peer cập nhật. Nhược điểm: nếu request chết giữa chừng (REST OK, WS publish fail) → peer lệch state.
> - **Phương án 2 (đúng chuẩn — cần bổ sung backend)**: Service activity sau khi commit DB sẽ `hub.BroadcastToRoom(itinerary_id, ...)` + `redisPubSub.Publish(...)`. Client chỉ cần lắng nghe. Khuyến nghị triển khai sớm.

### 5.4 Reconnect & lifecycle

```ts
function useItineraryWS(id: string, token: string, on: { ... }) {
  // - Tạo WS khi mount, đóng khi unmount.
  // - onclose → exponential backoff 1s, 2s, 4s, ... max 30s.
  // - ping/pong: server tự gửi ping mỗi 54s, browser auto-reply pong.
  // - Khi reconnect: re-fetch GET /itineraries/:id để đồng bộ state lỡ.
}
```

### 5.5 Optimistic update

Khi user kéo-thả hoặc edit:
1. Update state local ngay.
2. Gửi REST API.
3. Nếu API fail → rollback state + toast lỗi.
4. Nếu API OK → broadcast WS (phương án 1) HOẶC để backend broadcast (phương án 2).
5. Khi nhận lại event của chính mình (sender == self) → bỏ qua.

---

## 6. Publish & share

```
Owner page edit → click "Xuất bản"
  PATCH /itineraries/:id/publish → status=PUBLISHED, view_count reset
  Toast "Đã xuất bản. Link chia sẻ: <copy>"

Người khác mở /itinerary/:id/public:
  GET /itineraries/:id/public (không cần token)
  Render giống detail nhưng:
    - Ẩn nút Edit/Delete
    - Hiển thị nút "Lưu vào tài khoản" → nếu chưa login redirect /auth/login,
      đã login → POST /itineraries/:id/clone
    - Hiển thị nút Like (chưa có endpoint backend; cần thêm sau)
```

Mỗi lần `GET /itineraries/:id/public` thành công, service tăng `view_count` (xem `ItineraryService.GetPublic`).

---

## 7. Edge cases cần handle

| Vấn đề | Cách xử lý |
|---|---|
| 2 user cùng kéo-thả gần nhau → conflict order | Backend re-normalise order_index trong service Reorder. Sau khi nhận event mới, client áp lại. |
| Mất WS giữa khi đang kéo | Vẫn gửi REST. Sau reconnect re-fetch để đồng bộ. |
| Owner xoá itinerary trong khi có người đang xem | Backend trả 404; client hiện modal "Lịch trình đã bị xoá" → redirect /planner. |
| Token hết hạn khi đang trong WS | Server đóng connection. Client `onclose` → kiểm tra `/auth/me`, nếu 401 → redirect login. |
| Activity không có place_id (tự tạo) | Cho phép — vẫn lưu, hiển thị icon mặc định, không gắn lat/lng. |
| Publish khi chưa có activity nào | Backend cảnh báo (validate ở service); client disable nút nếu `activities.length === 0`. |
