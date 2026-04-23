# Phân việc — Frontend Dev (Next.js)

Công việc để frontend rời khỏi mock-data và chạy thật với backend + planner-ai. Các page shell đã có (thư mục `app/` đã scaffold), phần lớn việc là **thay mock → API thật + state management + logic UX**.

Sắp xếp theo priority P0 → P3. Mỗi task có DoD rõ ràng.

---

## P0 — Nền tảng (block mọi page khác)

### FE-1. `lib/api.ts` + types chung
- Implement `apiFetch<T>` theo mẫu ở [06-FRONTEND-INFRA.md §1](06-FRONTEND-INFRA.md#1-libapits).
- Tạo `lib/types.ts` với `User`, `Place`, `Activity`, `Itinerary`, `GenerateResponse`, `ChatMessage` (xem [§6](06-FRONTEND-INFRA.md#6-types-chung--libtypests)).
- Tạo `.env.local` với `NEXT_PUBLIC_API_URL`, `NEXT_PUBLIC_WS_URL`, `NEXT_PUBLIC_PLANNER_AI_URL`.
- **DoD**: Bất kỳ component nào gọi `apiFetch("/places")` đều chạy, 401 tự redirect login.

### FE-2. `AuthProvider` + `useAuth` + `RequireAuth`
- Tạo `hooks/use-auth.tsx`, bọc ở `app/layout.tsx`.
- Tạo `components/require-auth.tsx`.
- `useAuth` expose `{ user, token, login, loginGoogle, loginFacebook, logout, refresh }`.
- Bootstrap: khi mount → đọc token + `GET /auth/me`.
- **DoD**: F5 không mất user, 401 tự logout + redirect login.

### FE-3. Middleware protect routes
`middleware.ts` ở root frontend: chặn truy cập `/planner`, `/profile`, `/saved`, `/settings/*`, `/itinerary/new`, `/itinerary/[id]/edit`, `/ai-planner`, `/admin/*` khi không có cookie/token → redirect `/auth/login?redirect=<from>`.

---

## P1 — Auth pages (nối API thật)

### FE-4. `/auth/login`
- Form email + password → `useAuth().login`.
- Nút Google (Google Identity Services SDK) → `loginGoogle(credential)`.
- Nút Facebook (FB SDK) → gọi `POST /auth/facebook`.
- Link forgot-password (khi BE-7 có).
- Redirect về `?redirect=...` nếu có, mặc định `/planner`.

### FE-5. `/auth/register` + `/auth/verify`
- Register: validation mật khẩu (min 8, chữ + số), confirm password. Submit → toast → điều hướng `/auth/verify?email=...`.
- Verify: OTP 6 số (shadcn `input-otp` đã có). Nút "Gửi lại" với countdown 60s.
- **Lưu ý**: backend hiện dùng link token qua email (không phải OTP). Có 2 lựa chọn:
  - (a) Frontend chỉ hiển thị màn hình "Check mail" + nút resend, verify thực hiện qua link (backend redirect đến `/auth/verify?token=...`, frontend đọc query → `POST /auth/verify`).
  - (b) Yêu cầu backend đổi sang OTP. **Chốt với backend dev trước khi làm.**

### FE-6. `/auth/forgot-password` (khi BE-7 ready)
Form email → `POST /auth/forgot-password`, màn confirm.
`/auth/reset-password?token=...` → form new password → `POST /auth/reset-password`.

---

## P2 — User profile + saved

### FE-7. `/profile`
- Gọi `GET /user/profile` + `GET /itineraries` (tab lịch trình) + `GET /user/saved-places` (tab đã lưu).
- Form chỉnh sửa → `PATCH /user/profile`.
- Upload avatar: tạm dùng URL field, sau chuyển sang S3 presigned (phụ thuộc BE).

### FE-8. `/settings/security`
Form 3 input → `POST /user/change-password`. Sau thành công: `logout()` + toast.

### FE-9. `/saved`
- `GET /user/saved-places`, grid card.
- Nút bỏ lưu → `DELETE /user/saved-places/:place_id` + optimistic remove.
- Empty state link `/places`.

---

## P3 — Places & Combos

### FE-10. `/places` (list)
- Query từ URL params (shareable filter): `GET /places?destination=&category=&q=&tags=&min_rating=&page=`.
- Phụ thuộc BE-4. Trong khi chờ: lọc client-side tạm.
- Infinite scroll hoặc pagination.
- Toggle Map/Grid — component `ItineraryMap` đã có, tạo `PlacesMap` tương tự.

### FE-11. `/places/[id]`
- `GET /places/:id`. Gallery (swiper đã có), map embed (Leaflet).
- Tabs mô tả / đánh giá / combo liên quan / lịch trình có chứa.
- Sticky action: "Lưu" (`POST /user/saved-places`, toggle tim), "Thêm vào lịch trình" (mở Dialog chọn itinerary → `POST /activities`).

### FE-12. `/places/new` + `/places/[id]/edit` (admin)
- Form đầy đủ, map picker lat/lng, upload nhiều ảnh.
- Phụ thuộc admin middleware (BE-8).

### FE-13. `/combos` + `/combos/[id]` + `/combos/new`
- `GET /combos`, `GET /combos/:id`.
- Chi tiết: timeline ngày, bản đồ tổng, nút "Dùng combo" → tạo itinerary + activities (tương tự flow AI plan).
- Admin create/edit: form + picker.

---

## P4 — Itinerary edit + Realtime (trọng điểm UX)

### FE-14. `/itinerary/new` wizard
2 bước: (1) destination, dates, budget, guests; (2) tags, cover image → `POST /itineraries` → redirect `/itinerary/:id/edit`.

### FE-15. `/itinerary/[id]` detail
- `GET /itineraries/:id` kèm activities.
- Render timeline theo ngày, sidebar tabs ngày, budget recap, actions (Edit / Clone / Publish / Share).
- Clone: `POST /itineraries/:id/clone`. Publish: `PATCH /itineraries/:id/publish`.

### FE-16. `/itinerary/[id]/edit` — **DnD + form inline**
- `@dnd-kit/sortable` cho activity (package đã có).
- Mỗi activity là `<SortableItem>`. Kéo trong cùng ngày → đổi `order_index`. Kéo sang ngày khác → đổi `day_number`.
- Sau khi drop → `PATCH /activities/reorder` với **toàn bộ** activity bị ảnh hưởng (cả 2 ngày).
- Panel phải: form edit activity (autocomplete place qua `GET /places?q=`), save → `PATCH /activities/:id`.
- Nút thêm activity → Dialog: chọn từ Place list hoặc tự nhập → `POST /activities`.
- Autosave header (title/budget/dates) debounce 800ms → `PATCH /itineraries/:id`.

### FE-17. `useItineraryWS` hook + presence
- Tạo hook theo mẫu [06-FRONTEND-INFRA.md §4](06-FRONTEND-INFRA.md#4-hooksuse-itinerary-wsts).
- Lắng nghe `activity.*`, `itinerary.updated`, `presence.*`.
- Apply event vào state (skip nếu sender = self).
- Component `<PresenceStack/>` hiện avatar user đang online.
- Reconnect exponential backoff. Sau reconnect → re-fetch itinerary.
- Phụ thuộc BE-1 để event thực sự được broadcast.

### FE-18. `/itinerary/[id]/public` share page
- `GET /itineraries/:id/public` (không cần token).
- Ẩn actions edit/delete. Nút "Lưu về tài khoản" → nếu chưa login redirect `/auth/login?redirect=<this>`, đã login → `POST /itineraries/:id/clone`.

---

## P5 — AI Planner (trọng điểm sản phẩm)

### FE-19. `lib/stream-chat.ts` + `lib/tool-labels.ts`
Theo mẫu [06 §5](06-FRONTEND-INFRA.md#5-sse-helper-cho-chat) và [03 §6](03-AI-PLANNER-FLOW.md#6-tools-planner-ai-có-sẵn-để-frontend-hiển-thị-label-đúng).

### FE-20. `/ai-planner` — chat UI
- Layout 2 cột: sidebar session (`GET /sessions`, delete), main chat.
- Input auto-expand, Enter send, Shift+Enter newline, suggestion chips.
- Hiển thị:
  - Bubble user (phải) / AI (trái, markdown render)
  - Khi stream: typing dots + chip tool đang chạy (dùng `TOOL_LABELS`)
  - Khi done có `plan`: nhúng `<PlanPreviewCard/>` trong bubble AI
- Abort khi user gửi message mới giữa lúc đang stream.
- Sidebar session: click load → `GET /sessions/:id/history`. Delete → `DELETE /sessions/:id`.

### FE-21. `<PlacePicker/>` component (tính năng đặc biệt)
Theo [03-AI-PLANNER-FLOW.md §3](03-AI-PLANNER-FLOW.md#3-place-picker-ux--user-chọn-địa-điểm-muốn--không-muốn--để-ai-tự-quyết).

Flow:
1. Sau khi AI gợi ý địa điểm (hoặc user bấm nút "Chọn địa điểm trước"), component gọi `GET /places?destination=&category=ATTRACTION&limit=30`.
2. Render grid card, mỗi card 3 trạng thái: `include` / `exclude` / `neutral`.
3. Nút "AI tự chọn" reset toàn bộ.
4. Nút "Tạo lịch trình" → build hint message → `streamChat(...)` với message nhúng danh sách include/exclude (xem [03 §3.1](03-AI-PLANNER-FLOW.md#31-sketch-component-placepicker)).
5. Khi BE-2 xong → đổi sang truyền UUID qua API structured thay vì nhúng text.

### FE-22. `/ai-planner/quick` form
- Form đầy đủ: destination autocomplete, dates range, guests, budget (slider + input), tags multi-select, travel_style.
- Submit → `POST /api/v1/planner/generate` (qua `apiFetch` base=backend).
- Loading skeleton (có thể 30-90s).
- Render `<PlanPreviewCard/>` + nút "Lưu" / "Tạo lại".
- Xử lý 429 (rate-limit) hiển thị countdown.

### FE-23. `<PlanPreviewCard/>` + `savePlanAsItinerary`
- Hiển thị: budget recap, days timeline rút gọn, violations + warnings banner.
- Nút "Lưu thành lịch trình" → mở dialog nhập title → gọi helper `savePlanAsItinerary` ([03 §4](03-AI-PLANNER-FLOW.md#4-lưu-plan-thành-itinerary--chuẩn-hoá)).
- Khi BE-3 ready → đổi sang `POST /itineraries/from-plan`.

---

## P6 — Admin

### FE-24. `/admin` dashboard
Stats cơ bản (tổng user, tổng itinerary, top destinations). Cần API mới từ backend hoặc tự tính từ `GET /explore`.

### FE-25. `/admin/planner-cache`
Nút flush với confirm dialog → `DELETE /admin/planner/cache`.

### FE-26. `/admin/knowledge-base`
Drop zone upload JSON → preview → `POST /knowledge-base/seed`.

### FE-27. `/admin/places`, `/admin/combos`, `/admin/users`
CRUD table (sort, filter, pagination). Dùng component data-table (shadcn) có sẵn.

---

## P7 — Chất lượng & polish

- **FE-28**. Replace toàn bộ `mock-data.ts` bằng fetch thật (grep `from "@/lib/mock-data"`).
- **FE-29**. Loading skeleton cho mọi list/detail page.
- **FE-30**. Empty state có CTA ở `/saved`, `/planner`, `/ai-planner` (sidebar rỗng), `/explore` (0 kết quả).
- **FE-31**. Toast hệ thống: mọi mutation dùng `sonner` (đã cài).
- **FE-32**. Format tiền VND: helper `formatVND(n)` dùng `Intl.NumberFormat`.
- **FE-33**. Date format locale `vi` với `date-fns` (đã cài).
- **FE-34**. Responsive: test mobile cho `/itinerary/[id]/edit` (dnd kit mobile touch).
- **FE-35**. A11y: keyboard nav cho DnD (`@dnd-kit` hỗ trợ), aria-label cho icon-only buttons.
- **FE-36**. SEO meta tags cho `/explore`, `/places/[id]`, `/itinerary/[id]/public` (Next metadata API).
- **FE-37**. Sitemap + robots.txt.
- **FE-38**. Analytics: Vercel Analytics (đã cài `@vercel/analytics`) + event tracking cho login, create itinerary, save plan.
- **FE-39**. Error boundary + custom 404/500 pages.

---

## Phụ thuộc BE → FE (bảng)

| FE task | Chờ BE |
|---|---|
| FE-5 OTP verify | BE đổi sang OTP hoặc FE thích nghi với link token |
| FE-10 filter đầy đủ | BE-4 |
| FE-12 admin place CRUD | BE-8 role check |
| FE-17 WS realtime | BE-1 broadcast từ service |
| FE-21 PlacePicker structured | BE-2 |
| FE-23 save atomic | BE-3 |
| FE-24 admin dashboard | API stats mới |
| `/auth/forgot-password` | BE-7 |
| Like itinerary | BE-6 |
| Invitations UI | BE-5 collaborators |

---

## Thứ tự đề xuất sprint

- **Sprint 1 (1 tuần)**: FE-1 → FE-6 + FE-28 (strip mock). App chạy được end-to-end auth.
- **Sprint 2 (1 tuần)**: FE-7 → FE-13 (user + places + combos list/detail).
- **Sprint 3 (1-2 tuần)**: FE-14 → FE-18 (itinerary edit + realtime). Song song BE-1.
- **Sprint 4 (1-2 tuần)**: FE-19 → FE-23 (AI planner + place picker). Song song BE-2, BE-3.
- **Sprint 5 (1 tuần)**: FE-24 → FE-27 (admin) + polish.
- **Sprint 6**: FE-28 → FE-39 (QA, SEO, a11y, mobile).
