# 01 — API Contracts

Liệt kê **đầy đủ** endpoint frontend cần gọi, kèm payload và response mẫu. Mọi route protected yêu cầu header `Authorization: Bearer <jwt>`.

> Base URL backend: `${NEXT_PUBLIC_API_URL}` (mặc định `http://localhost:8080/api/v1`)
> Base URL planner-ai: `${NEXT_PUBLIC_PLANNER_AI_URL}` (mặc định `http://localhost:8001`)

---

## A. Backend (Go) — `/api/v1`

### A.1 Auth (public)

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/auth/register` | `{ email, password, full_name }` | `201 { user, token? }` (token chỉ khi không cần verify) |
| POST | `/auth/login` | `{ email, password }` | `200 { token, user }` |
| POST | `/auth/verify` | `{ token }` (token gửi qua email) | `200 { message }` |
| POST | `/auth/resend-verification` | `{ email }` | `200 { message }` (luôn 200 để tránh email enumeration) |
| POST | `/auth/google` | `{ id_token }` | `200 { token, user }` |
| POST | `/auth/facebook` | `{ access_token }` | `200 { token, user }` |
| GET | `/auth/me` 🔒 | — | `200 { user }` |

### A.2 User (🔒)

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/user/profile` | — | `{ user }` |
| PATCH | `/user/profile` | `{ full_name?, avatar_url?, bio?, phone? }` | `{ user }` |
| POST | `/user/change-password` | `{ old_password, new_password }` | `{ message }` |
| GET | `/user/saved-places` | — | `{ data: Place[] }` |
| POST | `/user/saved-places` | `{ place_id }` | `201 { message }` |
| DELETE | `/user/saved-places/:place_id` | — | `204` |

### A.3 Itineraries

| Method | Path | Body / Query | Response |
|---|---|---|---|
| GET | `/explore` (public) | `?destination=&budget_category=&tags=&min_budget=&max_budget=&sort=created_at&page=1&limit=20` | `{ data, total, page, limit }` |
| GET | `/itineraries/:id/public` | — | `Itinerary` (chỉ status=PUBLISHED) |
| GET | `/itineraries` 🔒 | — | `{ data: Itinerary[] }` |
| POST | `/itineraries` 🔒 | `CreateItineraryInput` (xem dưới) | `201 Itinerary` |
| GET | `/itineraries/:id` 🔒 | — | `Itinerary` (kèm activities) |
| PATCH | `/itineraries/:id` 🔒 | `UpdateItineraryInput` | `Itinerary` |
| DELETE | `/itineraries/:id` 🔒 | — | `204` |
| POST | `/itineraries/:id/clone` 🔒 | — | `201 Itinerary` (clone của user) |
| PATCH | `/itineraries/:id/publish` 🔒 | — | `Itinerary` (status: PUBLISHED) |

```ts
// CreateItineraryInput
{
  title: string;
  destination: string;          // "Đà Nẵng"
  budget: number;               // VND
  start_date: string;           // YYYY-MM-DD
  end_date: string;             // YYYY-MM-DD
  guest_count: number;
  tags?: string[];              // ["beach", "food"]
  budget_category?: "BUDGET" | "MODERATE" | "LUXURY";
  cover_image_url?: string;
  cloned_from_id?: string;      // khi clone từ explore
}
```

### A.4 Activities (🔒)

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/activities` | `CreateActivityInput` | `201 Activity` |
| PATCH | `/activities/:id` | `UpdateActivityInput` | `Activity` |
| DELETE | `/activities/:id` | — | `204` |
| PATCH | `/activities/reorder` | `{ items: [{ id, day_number, order_index }] }` | `{ message }` |

```ts
// CreateActivityInput
{
  itinerary_id: string;
  place_id?: string;            // null nếu là activity tự nhập (transit, ăn nhanh...)
  day_number: number;           // 1-based
  order_index: number;          // 0-based trong ngày
  title: string;
  category: "ATTRACTION" | "FOOD" | "STAY" | "TRANSPORT" | "ACTIVITY";
  start_time?: string;          // HH:MM
  end_time?: string;            // HH:MM
  estimated_cost?: number;
  lat?: number; lng?: number;
  image_url?: string;
  notes?: string;
}
```

### A.5 Places

| Method | Path | Query | Response |
|---|---|---|---|
| GET | `/places` (public) | `?destination=&category=ATTRACTION|FOOD|STAY&page=1&limit=20` | `{ data, total, page, limit }` |
| GET | `/places/:id` (public) | — | `Place` |
| POST | `/places` 🔒 | `CreatePlaceInput` | `201 Place` |
| PATCH | `/places/:id` 🔒 | `UpdatePlaceInput` | `Place` |
| DELETE | `/places/:id` 🔒 | — | `204` |

`Place` shape (rút gọn): xem [backend/internal/models/place.go](../../backend/internal/models/place.go).
Trường quan trọng cho UI: `id, name, category, destination, area, address, latitude, longitude, cover_image, images[], rating, review_count, must_visit, base_price, hours, recommended_duration, tags[]`.

### A.6 Combos

| Method | Path | Query | Response |
|---|---|---|---|
| GET | `/combos` (public) | `?destination=&page=&limit=` | `{ data, total }` |
| GET | `/combos/:id` (public) | — | `Combo` (kèm places và lịch trình mẫu) |
| POST/PATCH/DELETE | `/combos[...]` 🔒 | — | CRUD admin |

### A.7 Planner (proxy / engine)

| Method | Path | Body | Note |
|---|---|---|---|
| POST | `/planner/generate` (public, rate-limit 30/60s) | `GenerateRequest` | Trả `{ data: GenerateResponse }`. Header: `X-Planner-Mode: go-engine|llm`, `X-Cache: HIT|MISS` |
| DELETE | `/admin/planner/cache` 🔒 | — | Flush cache (admin) |

```ts
// GenerateRequest
{
  destination: string;
  start_date: string;             // YYYY-MM-DD
  end_date: string;
  budget_vnd: number;
  guest_count?: number;
  preference_tags?: string[];     // ["beach","food",...]
  travel_style?: "relaxed" | "standard" | "active";
  travel_month?: number;          // 1-12, suy ra từ start_date nếu thiếu
  arrival_time?: string;          // "10:00"
  departure_time?: string;        // "18:00"
}
```

```ts
// GenerateResponse — xem chi tiết file backend/internal/planner/types.go
{
  days: [{
    day_num, date_str, day_type, primary_area, travel_min, buffer_min,
    slots: [{
      start, end, slot_type, is_buffer, combo_covered?,
      place: { id, name, category, area, lat, lng, cover_image, base_price, duration_min, hours, is_must_visit, tags }
    }]
  }],
  budget_recap: { total_budget_vnd, attraction_spent_vnd, food_spent_vnd, remaining_vnd, within_budget },
  combo_result?: { use_combo, name, savings_vnd, savings_pct, ... },
  violations?: [{ rule, severity, message, day }],
  budget_tier: "survival" | "budget" | "standard" | "premium",
  budget_warning?: string,
  slot_template: "relaxed" | "standard" | "active",
  price_stale_warnings?: string[]
}
```

### A.8 Knowledge base (cho planner-ai)

| Method | Path | Query | Response |
|---|---|---|---|
| GET | `/knowledge-base/lookup` (public) | `?destination=&stale_days=30` | snapshot places + meta |
| POST | `/knowledge-base/seed` 🔒 | bulk JSON | import data |

### A.9 WebSocket

```
GET ws(s)://<host>/api/v1/ws/itinerary/:id?token=<jwt>
```
Auth bằng JWT trong query param vì browser WebSocket không gắn header được. Chi tiết protocol: [04-ITINERARY-COLLAB-FLOW.md](04-ITINERARY-COLLAB-FLOW.md).

---

## B. Planner-AI (FastAPI) — base `${NEXT_PUBLIC_PLANNER_AI_URL}`

### B.1 Chat

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/chat` | `{ session_id?, message }` | `ChatResponse` |
| POST | `/chat/stream` | `{ session_id?, message }` | SSE stream (events bên dưới) |

```ts
// ChatResponse
{
  session_id: string;                 // server tạo nếu thiếu
  response: string;                   // text từ AI
  plan?: { /* tương tự GenerateResponse */ } | null;  // có khi tool create_travel_plan được gọi
  tool_calls: string[];               // ["get_places", "create_travel_plan", ...]
  duration_ms: number;
}
```

**SSE events** (mỗi event là 1 dòng `data: <json>\n\n`):

```jsonc
// 1. AI bắt đầu gọi tool
{ "type": "tool_start", "tool": "get_places", "label": "Đang tra cứu địa điểm..." }

// 2. token streaming
{ "type": "token", "content": "Đà Nẵng " }

// 3. xong
{ "type": "done",
  "session_id": "...",
  "tool_calls": ["get_places", "create_travel_plan"],
  "plan": { ... },
  "full_text": "..."
}

// 4. lỗi
{ "type": "error", "message": "..." }
```

### B.2 Plan (structured one-shot)

| Method | Path | Body | Response |
|---|---|---|---|
| POST | `/plan` | `PlanRequest` | `PlanResponse` |

```ts
// PlanRequest
{
  destination: string;
  num_days: number;        // default 3
  start_date?: string;
  end_date?: string;
  budget_vnd?: number;     // 0 = tự phân bổ
  guest_count?: number;    // default 2
  raw_input?: string;      // free-text override
}

// PlanResponse
{
  session_id: string;
  destination: string;
  budget_tier: string;
  final_plan: { /* days, slots ... */ };
  budget_breakdown: {};
  warnings: string[];
  violations: { rule, severity, message, day? }[];
  validation_passed: boolean;
  duration_ms: number;
  cache_hit: boolean;
}
```

### B.3 Sessions

| Method | Path | Response |
|---|---|---|
| GET | `/sessions` | `SessionInfo[]` |
| GET | `/sessions/:id/history` | `{ session_id, messages, message_count, meta }` |
| DELETE | `/sessions/:id` | `{ deleted: true, session_id }` |

```ts
// SessionInfo
{ session_id: string; created_at?: string; last_active?: string; message_count: number; destination?: string }
```

### B.4 Cache & health

| Method | Path | Response |
|---|---|---|
| DELETE | `/cache` | `{ flushed: n }` |
| GET | `/health` | `{ status, service, version, redis }` |

---

## C. Mapping Frontend page → API

| Page | API gọi |
|---|---|
| `/auth/login` | `POST /auth/login`, `POST /auth/google`, `POST /auth/facebook` |
| `/auth/register` | `POST /auth/register` |
| `/auth/verify` | `POST /auth/verify`, `POST /auth/resend-verification` |
| `/profile` | `GET /user/profile`, `PATCH /user/profile`, `GET /itineraries`, `GET /user/saved-places` |
| `/settings/security` | `POST /user/change-password` |
| `/saved` | `GET /user/saved-places`, `DELETE /user/saved-places/:id` |
| `/explore` | `GET /explore` |
| `/places` | `GET /places` |
| `/places/[id]` | `GET /places/:id`, `POST /user/saved-places` |
| `/combos`, `/combos/[id]` | `GET /combos`, `GET /combos/:id` |
| `/planner` (dashboard) | `GET /itineraries` |
| `/itinerary/new` | `POST /itineraries` |
| `/itinerary/[id]` | `GET /itineraries/:id` |
| `/itinerary/[id]/edit` | `PATCH /itineraries/:id`, CRUD `/activities`, `PATCH /activities/reorder`, **WS** |
| `/itinerary/[id]/public` | `GET /itineraries/:id/public` |
| `/ai-planner` | `POST /chat/stream`, `GET/DELETE /sessions` |
| `/ai-planner/quick` | `POST /api/v1/planner/generate` |
| `/admin/planner-cache` | `DELETE /admin/planner/cache` |
| `/admin/knowledge-base` | `POST /knowledge-base/seed` |
| `/admin/places`, `/combos`, `/users` | CRUD tương ứng |
