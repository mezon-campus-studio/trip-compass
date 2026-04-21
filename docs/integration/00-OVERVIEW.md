# 00 — Tổng quan tích hợp TripCompass

Tài liệu này mô tả cách 3 service nói chuyện với nhau và file nào nên đọc theo từng nhu cầu.

## 1. Sơ đồ hệ thống

```
┌────────────────────┐    HTTPS/JSON      ┌────────────────────┐
│                    │  ───────────────►  │                    │
│  Frontend (Next.js)│                    │   Backend (Go/Gin) │
│   localhost:3000   │  ◄───────────────  │   localhost:8080   │
│                    │   JWT trong header │   /api/v1/*        │
└──────────┬─────────┘                    └──────────┬─────────┘
           │                                         │
           │ WebSocket (?token=jwt)                  │  Postgres + Redis
           │ /api/v1/ws/itinerary/:id                │
           │                                         │
           │  Chat / Plan trực tiếp                  │  Proxy /planner/generate
           │  (Server-Sent Events)                   │  khi USE_LLM_PLANNER=true
           └──────────────►┌────────────────────┐ ◄──┘
                           │  Planner-AI        │
                           │  (FastAPI/LangGraph)│
                           │  localhost:8001    │
                           └─────────┬──────────┘
                                     │ asyncpg + Redis
                                     ▼
                              Postgres (chung schema)
```

## 2. Cách mỗi cặp service nói chuyện

| Cặp | Giao thức | Khi nào dùng |
|---|---|---|
| Frontend → Backend | REST `/api/v1/*` (JSON, JWT Bearer) | CRUD user/itinerary/place/combo, auth, explore |
| Frontend ↔ Backend | WebSocket `/api/v1/ws/itinerary/:id?token=jwt` | Cộng tác realtime trên 1 itinerary (đa user) |
| Frontend → Planner-AI | REST `POST /chat`, `POST /chat/stream` (SSE), `GET /sessions/...` | Trợ lý chat tự nhiên, multi-turn |
| Frontend → Backend → Planner-AI | `POST /api/v1/planner/generate` (proxy) | Tạo plan structured one-shot, có rate-limit + cache backend |
| Backend → Planner-AI | HTTP nội bộ `POST /plan`, `DELETE /cache` | Khi `USE_LLM_PLANNER=true` |
| Planner-AI → Backend | `GET /api/v1/knowledge-base/lookup` | Lấy snapshot dữ liệu places/destination |
| Planner-AI → DB | asyncpg trực tiếp (read) | Tools `get_places`, `get_food_venues`, ... |

## 3. Biến môi trường (frontend `.env.local`)

```bash
NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1
NEXT_PUBLIC_WS_URL=ws://localhost:8080/api/v1/ws
NEXT_PUBLIC_PLANNER_AI_URL=http://localhost:8001
NEXT_PUBLIC_GOOGLE_CLIENT_ID=xxx.apps.googleusercontent.com
NEXT_PUBLIC_FACEBOOK_APP_ID=xxx
```

Backend `.env`:

```bash
USE_LLM_PLANNER=true|false
PLANNER_AI_URL=http://planner-ai:8001
ALLOWED_ORIGINS=http://localhost:3000,https://tripcompass.vn
JWT_SECRET=...
GOOGLE_CLIENT_ID=...
FACEBOOK_APP_SECRET=...
```

## 4. Quy ước chung

- **Auth header**: `Authorization: Bearer <jwt>` cho mọi endpoint protected.
- **Format response**:
  - List: `{ "data": [...], "total": n, "page": 1, "limit": 20 }`
  - Single object: trả thẳng object (vd `GET /itineraries/:id`) hoặc bọc `{ "user": {...} }` cho profile.
  - Error: `{ "error": "message" }` với HTTP status phù hợp (400/401/403/404/409/500).
- **UUID**: tất cả ID tài nguyên là UUID v4 string.
- **Date**: `start_date`, `end_date`, `DateOnly` dùng `YYYY-MM-DD`. Time-of-day dùng `HH:MM`.
- **Tiền tệ**: VND (integer). Field tên `*_vnd` hoặc `budget`/`base_price`.

## 5. Rate-limit & cache

- `POST /api/v1/planner/generate`: rate-limit 30 req/60s/IP (middleware `RateLimit`).
- Backend cache plan key SHA256 (destination + dates + budget bucket 100K + guest + tags), TTL 1h, **chỉ áp dụng khi `USE_LLM_PLANNER=false`**.
- Planner-AI tự cache bằng key `{destination}:{num_days}:{guests}:{budget}:{start_date}` (xem `app/services/plan_cache.py`).
- WebSocket: ping 54s, timeout pong 60s, max msg 8KB.

## 6. Thứ tự đọc

1. [01-API-CONTRACTS.md](01-API-CONTRACTS.md) — toàn bộ endpoint + payload mẫu
2. [02-AUTH-FLOW.md](02-AUTH-FLOW.md) — đăng ký / login / OAuth / JWT
3. [03-AI-PLANNER-FLOW.md](03-AI-PLANNER-FLOW.md) — **trọng điểm**: luồng tạo lịch trình bằng AI (chat + quick + place-picker)
4. [04-ITINERARY-COLLAB-FLOW.md](04-ITINERARY-COLLAB-FLOW.md) — CRUD + drag-drop + WebSocket realtime
5. [05-PLACES-COMBOS-FLOW.md](05-PLACES-COMBOS-FLOW.md) — duyệt place/combo, lưu, gắn vào lịch trình
6. [06-FRONTEND-INFRA.md](06-FRONTEND-INFRA.md) — `lib/api.ts`, `useAuth`, `useItineraryWS`, error handling
