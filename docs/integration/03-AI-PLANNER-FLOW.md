# 03 — Luồng tạo lịch trình bằng AI (trọng điểm)

Hai con đường tạo lịch trình bằng AI từ frontend:

| Đường | Endpoint | Khi nào dùng |
|---|---|---|
| **A. Quick form** (1 lần gọi) | `POST /api/v1/planner/generate` (proxy) hoặc `POST {planner-ai}/plan` trực tiếp | User có sẵn ý: điểm đến, ngày, ngân sách → muốn nhận plan ngay |
| **B. Chat đa lượt** (streaming) | `POST {planner-ai}/chat/stream` | User muốn hỏi đáp, gợi ý, lọc địa điểm trước khi xác nhận |

Bài này tập trung **Đường B kèm cơ chế "chọn địa điểm muốn / không muốn"** vì đây là UX chính sản phẩm yêu cầu.

---

## 1. Đường A — Quick form `/ai-planner/quick`

```
User                Frontend                    Backend                Planner-AI
 │ Điền form         │                              │                      │
 │ destination, days │                              │                      │
 │ budget, guests    │                              │                      │
 │ tags[], dates     │                              │                      │
 │──────────────────►│                              │                      │
 │ Submit            │ POST /api/v1/planner/generate│                      │
 │                   │  (rate-limit 30/60s)         │                      │
 │                   │─────────────────────────────►│                      │
 │                   │                              │ if USE_LLM_PLANNER:  │
 │                   │                              │   POST /plan ────────►│
 │                   │                              │                      │ pipeline:
 │                   │                              │                      │ resolve→gather
 │                   │                              │                      │ →budget→schedule
 │                   │                              │                      │ →validate→enrich
 │                   │                              │                      │ (cache nếu hit)
 │                   │                              │                      │
 │                   │                              │  else:               │
 │                   │                              │   Go engine          │
 │                   │                              │   (planner.Engine)   │
 │                   │                              │   (cache backend)    │
 │                   │ 200 { data: GenerateResponse}│                      │
 │                   │◄─────────────────────────────│                      │
 │ Loading skeleton  │                              │                      │
 │ (5-90s với LLM)   │                              │                      │
 │ → render plan     │                              │                      │
 │ Nút "Lưu lịch     │                              │                      │
 │   trình"          │                              │                      │
 │──────────────────►│ POST /itineraries            │                      │
 │                   │ (title, destination, dates,  │                      │
 │                   │  budget, guests, tags)       │                      │
 │                   │─────────────────────────────►│                      │
 │                   │ 201 { id }                   │                      │
 │                   │◄─────────────────────────────│                      │
 │                   │ Loop activities theo days[]: │                      │
 │                   │ POST /activities ×N          │                      │
 │                   │─────────────────────────────►│                      │
 │                   │ redirect /itinerary/:id      │                      │
```

**Lưu ý**:
- Response từ planner trả `days[].slots[].place` đã có `id` (UUID). Khi lưu thành activity, **set `place_id = place.id`**, lấy `category` từ `place.category`, `start_time`/`end_time` từ slot.
- Nếu `slot.is_buffer = true` → bỏ qua hoặc tạo activity category=ACTIVITY ghi chú "di chuyển/nghỉ".
- Cảnh báo `violations[]` và `price_stale_warnings[]` hiển thị ở banner trên cùng plan preview.

---

## 2. Đường B — Chat đa lượt `/ai-planner`

### 2.1 Khởi tạo phiên

```
Mở /ai-planner:
  GET /sessions          → render sidebar (list session cũ)
  Sidebar item click:
    GET /sessions/:id/history → load messages + meta
    set session_id local
  "Cuộc trò chuyện mới":
    session_id = null (planner-ai sẽ tạo mới sau khi user gửi message đầu)
```

### 2.2 Vòng đời 1 message (SSE)

```
User gõ:                            Frontend                    Planner-AI
"Cho tôi gợi ý chỗ chơi Đà Nẵng"
─────────────────────────────────►   │
                                     │ POST /chat/stream
                                     │ { session_id?, message }
                                     │──────────────────────────►│
                                     │                           │ load history (Redis)
                                     │                           │ build messages
                                     │                           │ invoke chat_agent
                                     │                           │
                                     │   data:{type:"tool_start",│
                                     │       tool:"get_places",  │
                                     │       label:"Tra cứu..."} │
                                     │◄──────────────────────────│
                                     │ Hiện chip "Đang tra cứu   │
                                     │   địa điểm..."            │
                                     │                           │
                                     │   data:{type:"token",     │
                                     │       content:"Đà Nẵng "} │
                                     │◄──────────────────────────│ ... (nhiều token)
                                     │ Append vào bubble AI       │
                                     │                           │
                                     │   data:{type:"done",      │
                                     │       session_id, plan?,  │
                                     │       tool_calls,         │
                                     │       full_text}          │
                                     │◄──────────────────────────│
                                     │ Lưu session_id (nếu mới)  │
                                     │ Nếu có plan → render      │
                                     │ <PlanPreviewCard/>        │
                                     │ trong bubble AI            │
```

**Client SSE mẫu**:

```ts
async function streamChat(sessionId: string | null, message: string, on: {
  toolStart: (tool: string, label?: string) => void;
  token: (text: string) => void;
  done: (sessionId: string, fullText: string, plan?: any) => void;
  error: (msg: string) => void;
}) {
  const res = await fetch(`${PLANNER_AI}/chat/stream`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, message }),
  });
  const reader = res.body!.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let idx;
    while ((idx = buffer.indexOf("\n\n")) >= 0) {
      const raw = buffer.slice(0, idx); buffer = buffer.slice(idx + 2);
      if (!raw.startsWith("data: ")) continue;
      const evt = JSON.parse(raw.slice(6));
      if (evt.type === "tool_start") on.toolStart(evt.tool, evt.label);
      else if (evt.type === "token") on.token(evt.content);
      else if (evt.type === "done") on.done(evt.session_id, evt.full_text, evt.plan);
      else if (evt.type === "error") on.error(evt.message);
    }
  }
}
```

---

## 3. **Place-picker UX** — User chọn địa điểm muốn / không muốn / để AI tự quyết

UX này là tổ hợp **2 lượt chat + 1 form chọn** chứ không phải 1 endpoint duy nhất. Flow chuẩn:

```
                    Frontend                                    Planner-AI / Backend
                    
1. User gõ "Tôi muốn đi Đà Nẵng 3 ngày, ngân sách 5tr cho 2 người"
   ───────────────────────────►
                    │ POST /chat/stream
                    │  {message: <ý định>}
                    │──────────────────────────────────────────►│ AI hiểu intent.
                    │                                           │ Gọi tool get_places(destination=...)
                    │                                           │ Trả text gợi ý + tool_calls
                    │ Done event                                │
                    │◄──────────────────────────────────────────│
                    │ AI: "Đây là các điểm nổi bật ở Đà Nẵng,
                    │ bạn muốn đi chỗ nào?"
                    │
2. Frontend RIÊNG BIỆT gọi backend lấy danh sách place đầy đủ kèm ảnh:
                    │ GET /api/v1/places?destination=da+nang
                    │ &category=ATTRACTION&limit=30
                    │──────────────────────────────────────────►│ Backend
                    │ { data: Place[] }                         │
                    │◄──────────────────────────────────────────│
                    │ Render PlacePicker UI:
                    │   - Mỗi card có 3 nút: "Muốn đi" / "Bỏ qua" / chưa chọn
                    │   - Nút tổng "AI tự chọn cho tôi"
                    │   - Nút "Tạo lịch trình"

3. User chọn xong (must_include=[id1,id3], exclude=[id5]) bấm "Tạo lịch trình":
                    │ POST /chat/stream
                    │  message: "Tạo lịch trình 3 ngày cho 2
                    │   người, 5tr. BẮT BUỘC ghé: <tên các place
                    │   must_include>. KHÔNG ghé: <tên exclude>.
                    │   Bắt đầu 2026-05-01."
                    │──────────────────────────────────────────►│ AI gọi create_travel_plan(...)
                    │                                           │ Tool đọc places → schedule
                    │                                           │ → validate → enrich
                    │ token... done {plan: {...}}               │
                    │◄──────────────────────────────────────────│
                    │ Render <PlanPreviewCard/> trong chat
                    │
4. User bấm "Lưu thành lịch trình":
                    │ POST /api/v1/itineraries (Backend)
                    │   { title, destination, start_date,
                    │     end_date, budget, guest_count,
                    │     tags, budget_category }
                    │──────────────────────────────────────────►│
                    │ 201 { id }                                │
                    │◄──────────────────────────────────────────│
                    │ Loop plan.days[].slots[]:
                    │   POST /api/v1/activities                 │
                    │   {itinerary_id, place_id, day_number,    │
                    │    order_index, title, category,          │
                    │    start_time, end_time, estimated_cost,  │
                    │    lat, lng, image_url, notes}            │
                    │──────────────────────────────────────────►│
                    │ redirect /itinerary/:id/edit
```

> **Quan trọng**: hiện tại tool `create_travel_plan` ở planner-ai **chưa nhận tham số `must_include_place_ids` / `exclude_place_ids`**. Có 2 cách triển khai:
>
> 1. **Tạm thời (frontend chỉ truyền tự nhiên)**: Nhúng tên các place đã chọn vào message (như flow trên). LLM hiểu và ưu tiên. Đơn giản, không sửa backend, nhưng độ chính xác phụ thuộc LLM.
> 2. **Đúng chuẩn (cần bổ sung)**: Mở rộng `tools/create_plan.py` thêm 2 tham số `must_include`, `exclude` (list[str] — UUID hoặc name), sau đó propagate xuống `node_schedule`. Frontend gọi `POST /chat` với 1 hint format máy đọc, hoặc thêm hẳn endpoint REST `POST /plan/with-prefs` nhận body có 2 field này. Khuyến nghị làm bước này khi UX yêu cầu chính xác cao.

### 3.1 Sketch component PlacePicker

```tsx
// components/place-picker.tsx
type Pick = "include" | "exclude" | "neutral";

const [picks, setPicks] = useState<Record<string, Pick>>({});
const include = Object.entries(picks).filter(([_, v]) => v === "include").map(([k]) => k);
const exclude = Object.entries(picks).filter(([_, v]) => v === "exclude").map(([k]) => k);

// Bấm "AI tự chọn" → reset toàn bộ về "neutral"
// Bấm "Tạo lịch trình" → build hint message:
const includeNames = places.filter(p => include.includes(p.id)).map(p => p.name).join(", ");
const excludeNames = places.filter(p => exclude.includes(p.id)).map(p => p.name).join(", ");
const hint = [
  `Tạo lịch trình ${days} ngày tại ${destination}, ${guestCount} người, ngân sách ${budget} VND.`,
  includeNames && `BẮT BUỘC ghé: ${includeNames}.`,
  excludeNames && `TUYỆT ĐỐI KHÔNG đưa vào: ${excludeNames}.`,
  startDate && `Bắt đầu ${startDate}.`,
].filter(Boolean).join(" ");

await streamChat(sessionId, hint, callbacks);
```

---

## 4. Lưu plan thành Itinerary — chuẩn hoá

Tạo helper `lib/plan-to-itinerary.ts`:

```ts
import { apiFetch } from "@/lib/api";

export async function savePlanAsItinerary(plan: GenerateResponse, meta: {
  title: string;
  destination: string;
  start_date: string;
  end_date: string;
  budget_vnd: number;
  guest_count: number;
  tags: string[];
}) {
  // 1. Tạo itinerary rỗng
  const it = await apiFetch<Itinerary>("/itineraries", {
    method: "POST",
    body: {
      title: meta.title,
      destination: meta.destination,
      start_date: meta.start_date,
      end_date: meta.end_date,
      budget: meta.budget_vnd,
      guest_count: meta.guest_count,
      tags: meta.tags,
      budget_category: tierToCategory(plan.budget_tier),
    },
  });

  // 2. Tạo activities tuần tự (giữ thứ tự đảm bảo unique constraint
  //    (itinerary_id, day_number, order_index))
  for (const day of plan.days) {
    let order = 0;
    for (const slot of day.slots) {
      if (slot.is_buffer || !slot.place) continue;
      await apiFetch("/activities", {
        method: "POST",
        body: {
          itinerary_id: it.id,
          place_id: slot.place.id,
          day_number: day.day_num,
          order_index: order++,
          title: slot.place.name,
          category: slot.place.category,
          start_time: slot.start,
          end_time: slot.end,
          estimated_cost: slot.place.base_price,
          lat: slot.place.lat,
          lng: slot.place.lng,
          image_url: slot.place.cover_image,
          notes: slot.combo_covered ? "Đã bao gồm trong combo" : undefined,
        },
      });
    }
  }

  return it;
}

function tierToCategory(tier: string): "BUDGET" | "MODERATE" | "LUXURY" {
  if (tier === "premium") return "LUXURY";
  if (tier === "survival" || tier === "budget") return "BUDGET";
  return "MODERATE";
}
```

> Có thể cải tiến bằng endpoint backend `POST /itineraries/from-plan` nhận luôn `plan` JSON và tạo cả itinerary + activities trong 1 transaction. Hiện chưa có — nếu cần atomic, hãy bổ sung.

---

## 5. Trạng thái UI cần xử lý

| Trạng thái | Hiển thị |
|---|---|
| Đang stream (chưa done) | Bubble AI + dấu "..." typing + chip tool đang chạy |
| Tool `get_places` chạy | "🔎 Đang tra cứu địa điểm..." |
| Tool `get_food_venues` | "🍜 Đang tìm quán ăn..." |
| Tool `get_weather` | "☀️ Đang xem thời tiết..." |
| Tool `search_hotels` | "🏨 Đang tìm khách sạn..." |
| Tool `create_travel_plan` | "📅 Đang lên lịch trình chi tiết..." (có thể mất 30-90s) |
| Done có `plan` | Render `PlanPreviewCard` kèm nút "Lưu" / "Tinh chỉnh" / "Tạo lại" |
| Done không có `plan` | Chỉ render text |
| Error | Bubble đỏ + nút "Thử lại" (gửi lại message gần nhất) |
| Mất kết nối SSE | Hiện banner "Mất kết nối, đang thử lại..." + auto retry sau 3s |
| Quota hit (HTTP 429) | "Bạn đã gửi quá nhanh. Thử lại sau 60s" |

---

## 6. Tools planner-ai có sẵn (để frontend hiển thị label đúng)

Xem [planner-ai/app/tools/](../../planner-ai/app/tools/):

| Tool name | Mục đích | Label tiếng Việt |
|---|---|---|
| `get_places` | Lấy attractions ở destination | "Đang tra cứu địa điểm tham quan..." |
| `get_food_venues` | Lấy quán ăn | "Đang tìm món ngon..." |
| `get_combos` | Lấy combo tour | "Đang gợi ý combo tour..." |
| `get_weather` | Thời tiết theo tháng | "Đang kiểm tra thời tiết..." |
| `get_real_prices` | Giá thực tế | "Đang cập nhật giá..." |
| `search_hotels` | Tìm khách sạn | "Đang tìm khách sạn..." |
| `search_flights` | Tìm chuyến bay | "Đang tìm chuyến bay..." |
| `create_travel_plan` | Lên lịch trình hoàn chỉnh | "Đang lên lịch trình chi tiết..." |

Map trong `lib/tool-labels.ts`:

```ts
export const TOOL_LABELS: Record<string, { vi: string; icon: string }> = {
  get_places:        { vi: "Đang tra cứu địa điểm tham quan...", icon: "🏝️" },
  get_food_venues:   { vi: "Đang tìm món ngon...",                icon: "🍜" },
  get_combos:        { vi: "Đang gợi ý combo tour...",            icon: "🎁" },
  get_weather:       { vi: "Đang kiểm tra thời tiết...",          icon: "☀️" },
  get_real_prices:   { vi: "Đang cập nhật giá...",                icon: "💵" },
  search_hotels:     { vi: "Đang tìm khách sạn...",               icon: "🏨" },
  search_flights:    { vi: "Đang tìm chuyến bay...",              icon: "✈️" },
  create_travel_plan:{ vi: "Đang lên lịch trình chi tiết...",     icon: "📅" },
};
```

---

## 7. Suggested chips ở khung input chat

```ts
const SUGGESTIONS = [
  "Đi Đà Nẵng 3 ngày cho 2 người, ngân sách 5tr",
  "Quán ăn ngon nhất Đà Lạt",
  "Combo tour tiết kiệm Phú Quốc",
  "Lịch trình cho cặp đôi Hội An 2N1Đ",
  "Đi Sapa đầu tháng 12 nên mặc gì?",
];
```
