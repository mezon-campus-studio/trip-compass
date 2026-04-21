# 05 — Places, Combos, Saved & Explore

## 1. Khám phá địa điểm `/places`

```
GET /places?destination=&category=&page=1&limit=20
  → { data: Place[], total, page, limit }

UI:
  - Sidebar filter:
      destination (dropdown), category (ATTRACTION|FOOD|STAY),
      price range, rating ≥, must_visit toggle, tags multi-select
  - Card place: cover_image, name, area, rating + review_count,
    must_visit badge, base_price, hours, nút "Lưu" 🤍 / ❤️
  - Toggle Grid / Map view (Leaflet)
  - Pagination hoặc infinite scroll
```

**Filter chưa có ở backend** (cần bổ sung khi cần): `tags`, `min_rating`, `q` (search). Tạm thời lọc client-side sau khi nhận `data`.

## 2. Chi tiết `/places/[id]`

```
GET /places/:id → Place
Render:
  - Gallery: cover_image + images[]
  - Info: name, name_en, area, address, hours, base_price, rating,
    duration, best_time_of_day, tags
  - Map (lat/lng) embed
  - Description (markdown)
  - Tabs:
      "Mô tả" — description
      "Lịch trình liên quan" — GET /explore?destination=<dest> (lọc client)
      "Combo có địa điểm này" — GET /combos?destination=<dest> (lọc client)
  - Sticky actions: "Lưu" (POST /user/saved-places), "Thêm vào lịch trình"
```

### 2.1 "Thêm vào lịch trình" từ trang place

```
User click "Thêm vào lịch trình":
  Nếu chưa login → redirect /auth/login?redirect=<place url>
  Nếu đã login   → mở Dialog liệt kê itinerary (GET /itineraries):
    - Chọn 1 itinerary có sẵn → POST /activities
        { itinerary_id, place_id, day_number: <chọn>, order_index: <last+1>,
          title: place.name, category: place.category, ... }
    - Hoặc "Tạo lịch trình mới" → wizard nhanh (title, dates, budget) →
        POST /itineraries → POST /activities
```

## 3. Saved Places `/saved`

```
GET /user/saved-places → { data: Place[] }
UI:
  - Grid card (tái dùng <PlaceCard/>)
  - Hover/tap → nút bỏ lưu: DELETE /user/saved-places/:place_id
  - Filter theo category, search
  - Empty state: "Chưa lưu địa điểm nào"
```

## 4. Combos `/combos`

```
GET /combos?destination=&page=&limit= → { data: Combo[], total }
UI: card combo (ảnh, tên, days, total_cost, num_places, badge giảm giá)
```

### 4.1 Chi tiết combo `/combos/[id]`

```
GET /combos/:id → Combo (kèm places hoặc lịch trình mẫu)
Render:
  - Hero ảnh + summary: days, total_cost, savings_pct nếu có
  - Timeline mẫu các ngày
  - Bản đồ tổng thể
  - Nút "Dùng combo này" → tạo itinerary từ combo:
      POST /itineraries { title, destination, dates, budget, ... }
      Loop combo.days[] → POST /activities (giống flow AI plan)
      redirect /itinerary/:id/edit
```

## 5. Explore lịch trình `/explore`

```
GET /explore?destination=&budget_category=&tags=&min_budget=&max_budget=
            &sort=created_at|popular|rating&page=1&limit=20
→ { data: Itinerary[], total, page, limit }

UI:
  - Filter: destination, duration (suy ra từ start/end), budget_category, tags
  - Card itinerary (tái dùng <ItineraryCard/>): cover, title, destination,
    duration, view_count, rating, owner avatar
  - Click → /itinerary/[id]/public
  - Mỗi card có nút "Lưu vào tài khoản" → POST /itineraries/:id/clone
```

## 6. Mối liên hệ giữa các tài nguyên

```
User
 ├─ owns Itinerary[] (status DRAFT/PUBLISHED)
 │    └─ has Activity[] (mỗi activity có place_id?)
 ├─ saves Place[] (user_saved_place)
 └─ collaborates on Itinerary[] (collaborator status)

Place
 ├─ thuộc destination (string), category enum
 ├─ tham chiếu bởi Activity.place_id
 └─ tham chiếu bởi Combo.places[]

Combo
 ├─ destination
 └─ places[] (denormalized hoặc bảng nối)

Itinerary (PUBLISHED) hiển thị ở /explore.
Itinerary clone tăng clone_count, set cloned_from_id.
```

## 7. Knowledge base lookup (chỉ planner-ai dùng)

`GET /api/v1/knowledge-base/lookup?destination=da+nang&stale_days=30` trả snapshot tổng hợp (places + meta) cho planner-ai. Frontend **không** gọi trực tiếp — chỉ dùng nếu xây admin tool xem health của KB.
