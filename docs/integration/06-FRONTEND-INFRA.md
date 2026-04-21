# 06 — Frontend infra: API client, hooks, error handling

Hướng dẫn các module nền tảng để mọi page tích hợp đồng nhất.

## 1. `lib/api.ts` — fetch wrapper

```ts
// lib/api.ts
const API   = process.env.NEXT_PUBLIC_API_URL!;        // http://localhost:8080/api/v1
const AI    = process.env.NEXT_PUBLIC_PLANNER_AI_URL!; // http://localhost:8001

type ReqOpts = Omit<RequestInit, "body"> & {
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined>;
  base?: "backend" | "ai";
  auth?: boolean;     // gắn JWT, default true
  raw?: boolean;      // không parse JSON (dùng cho stream)
};

class ApiError extends Error {
  constructor(public status: number, public data: any, message: string) {
    super(message);
  }
}

function buildUrl(base: "backend" | "ai", path: string, query?: ReqOpts["query"]) {
  const root = base === "ai" ? AI : API;
  const url = new URL(root.replace(/\/$/, "") + path);
  if (query) for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== null && v !== "") url.searchParams.set(k, String(v));
  }
  return url.toString();
}

function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem("token");
}

export async function apiFetch<T = any>(path: string, opts: ReqOpts = {}): Promise<T> {
  const { body, query, base = "backend", auth = true, raw = false, headers, ...rest } = opts;
  const h = new Headers(headers);
  if (body && !(body instanceof FormData)) h.set("Content-Type", "application/json");
  if (auth) {
    const t = getToken();
    if (t) h.set("Authorization", `Bearer ${t}`);
  }

  const res = await fetch(buildUrl(base, path, query), {
    ...rest,
    headers: h,
    body: body instanceof FormData ? body : body ? JSON.stringify(body) : undefined,
  });

  if (raw) return res as unknown as T;

  if (res.status === 401 && auth) {
    // Token sai/hết hạn — clear + redirect login
    localStorage.removeItem("token");
    if (typeof window !== "undefined") {
      const here = window.location.pathname + window.location.search;
      window.location.href = `/auth/login?redirect=${encodeURIComponent(here)}`;
    }
    throw new ApiError(401, null, "unauthorized");
  }

  if (res.status === 204) return undefined as T;

  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data, data?.error ?? `HTTP ${res.status}`);
  return data as T;
}

export { ApiError };
```

**Sử dụng**:

```ts
// GET với query
const { data } = await apiFetch<{ data: Place[] }>("/places", {
  query: { destination: "Đà Nẵng", category: "ATTRACTION", page: 1, limit: 20 },
});

// POST
const it = await apiFetch<Itinerary>("/itineraries", {
  method: "POST",
  body: { title, destination, start_date, end_date, budget, guest_count },
});

// Gọi planner-ai
await apiFetch("/sessions", { base: "ai", auth: false });

// Stream (SSE)
const res = await apiFetch<Response>("/chat/stream", {
  base: "ai", auth: false, method: "POST", raw: true,
  body: { session_id: sid, message },
});
// res.body.getReader() ...
```

## 2. `hooks/use-auth.tsx`

```tsx
"use client";
import { createContext, useContext, useEffect, useState } from "react";
import { apiFetch } from "@/lib/api";

type User = { id: string; email: string; full_name: string; avatar_url?: string };
type Ctx = {
  user: User | null;
  token: string | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  loginGoogle: (id_token: string) => Promise<void>;
  logout: () => void;
  refresh: () => Promise<void>;
};

const AuthContext = createContext<Ctx | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = async () => {
    const t = localStorage.getItem("token");
    if (!t) { setUser(null); setToken(null); return; }
    try {
      const { user } = await apiFetch<{ user: User }>("/auth/me");
      setUser(user); setToken(t);
    } catch {
      localStorage.removeItem("token"); setUser(null); setToken(null);
    }
  };

  useEffect(() => { refresh().finally(() => setLoading(false)); }, []);

  const persist = (t: string, u: User) => {
    localStorage.setItem("token", t); setToken(t); setUser(u);
  };

  const login = async (email: string, password: string) => {
    const { token, user } = await apiFetch<{ token: string; user: User }>("/auth/login", {
      method: "POST", body: { email, password }, auth: false,
    });
    persist(token, user);
  };

  const loginGoogle = async (id_token: string) => {
    const { token, user } = await apiFetch<{ token: string; user: User }>("/auth/google", {
      method: "POST", body: { id_token }, auth: false,
    });
    persist(token, user);
  };

  const logout = () => {
    localStorage.removeItem("token"); setUser(null); setToken(null);
    window.location.href = "/";
  };

  return (
    <AuthContext.Provider value={{ user, token, loading, login, loginGoogle, logout, refresh }}>
      {children}
    </AuthContext.Provider>
  );
}

export const useAuth = () => {
  const c = useContext(AuthContext);
  if (!c) throw new Error("useAuth phải dùng trong AuthProvider");
  return c;
};
```

Wrap ở `app/layout.tsx`:

```tsx
<AuthProvider>{children}</AuthProvider>
```

## 3. `components/require-auth.tsx`

```tsx
"use client";
import { useAuth } from "@/hooks/use-auth";
import { useRouter, usePathname } from "next/navigation";
import { useEffect } from "react";

export function RequireAuth({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();
  const router = useRouter();
  const path = usePathname();

  useEffect(() => {
    if (!loading && !user) router.replace(`/auth/login?redirect=${encodeURIComponent(path)}`);
  }, [loading, user, path, router]);

  if (loading || !user) return null; // hoặc <Skeleton/>
  return <>{children}</>;
}
```

## 4. `hooks/use-itinerary-ws.ts`

```ts
"use client";
import { useEffect, useRef } from "react";
import { useAuth } from "@/hooks/use-auth";

type WSEvent =
  | { type: "activity.created"; payload: { activity: any }; sender?: any }
  | { type: "activity.updated"; payload: { activity: any }; sender?: any }
  | { type: "activity.deleted"; payload: { activity_id: string }; sender?: any }
  | { type: "activity.reordered"; payload: { items: any[] }; sender?: any }
  | { type: "itinerary.updated"; payload: { itinerary: any }; sender?: any }
  | { type: "presence.join" | "presence.leave"; payload: { user_id: string; full_name?: string } }
  | { type: "error"; payload: { message: string } };

export function useItineraryWS(itineraryId: string, onEvent: (e: WSEvent) => void) {
  const { token } = useAuth();
  const wsRef = useRef<WebSocket | null>(null);
  const retryRef = useRef(0);

  useEffect(() => {
    if (!token || !itineraryId) return;
    let cancelled = false;

    const connect = () => {
      const url = `${process.env.NEXT_PUBLIC_WS_URL}/itinerary/${itineraryId}?token=${encodeURIComponent(token)}`;
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen   = () => { retryRef.current = 0; };
      ws.onmessage = (m) => {
        try { onEvent(JSON.parse(m.data) as WSEvent); }
        catch { /* ignore */ }
      };
      ws.onclose = () => {
        if (cancelled) return;
        const delay = Math.min(30000, 1000 * 2 ** retryRef.current++);
        setTimeout(connect, delay);
      };
      ws.onerror = () => ws.close();
    };

    connect();
    return () => { cancelled = true; wsRef.current?.close(); };
  }, [itineraryId, token, onEvent]);

  const send = (msg: object) => wsRef.current?.send(JSON.stringify(msg));
  return { send };
}
```

## 5. SSE helper cho chat

```ts
// lib/stream-chat.ts
export type StreamHandlers = {
  onToolStart?: (tool: string, label?: string) => void;
  onToken?:     (text: string) => void;
  onDone:       (sessionId: string, fullText: string, plan?: any, tools?: string[]) => void;
  onError?:     (msg: string) => void;
  signal?:      AbortSignal;
};

export async function streamChat(
  sessionId: string | null, message: string, h: StreamHandlers
) {
  const res = await fetch(`${process.env.NEXT_PUBLIC_PLANNER_AI_URL}/chat/stream`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, message }),
    signal: h.signal,
  });
  if (!res.ok || !res.body) throw new Error(`stream failed ${res.status}`);

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    let i;
    while ((i = buf.indexOf("\n\n")) >= 0) {
      const chunk = buf.slice(0, i); buf = buf.slice(i + 2);
      if (!chunk.startsWith("data: ")) continue;
      let evt: any;
      try { evt = JSON.parse(chunk.slice(6)); } catch { continue; }
      switch (evt.type) {
        case "tool_start": h.onToolStart?.(evt.tool, evt.label); break;
        case "token":      h.onToken?.(evt.content ?? ""); break;
        case "done":       h.onDone(evt.session_id, evt.full_text ?? "", evt.plan, evt.tool_calls); break;
        case "error":      h.onError?.(evt.message ?? "unknown"); break;
      }
    }
  }
}
```

## 6. Types chung — `lib/types.ts`

```ts
export type User = {
  id: string; email: string; full_name: string;
  avatar_url?: string; bio?: string; phone?: string; created_at: string;
};

export type Place = {
  id: string; destination: string;
  category: "ATTRACTION" | "FOOD" | "STAY";
  name: string; name_en?: string;
  description?: string; address?: string; area?: string;
  latitude?: number; longitude?: number;
  cover_image?: string; images: string[];
  rating?: number; review_count: number;
  must_visit: boolean; priority_score: number;
  best_time_of_day?: string; tags: string[];
  open_time?: string; close_time?: string; hours?: string;
  recommended_duration?: number; base_price?: number;
  phone?: string; website?: string;
};

export type Activity = {
  id: string; itinerary_id: string; place_id?: string;
  day_number: number; order_index: number;
  title: string; category: string;
  lat?: number; lng?: number;
  estimated_cost: number;
  start_time?: string; end_time?: string;
  image_url?: string; notes?: string;
};

export type Itinerary = {
  id: string; owner_id: string;
  title: string; destination: string;
  budget: number; start_date: string; end_date: string;
  status: "DRAFT" | "PUBLISHED";
  cover_image_url?: string; rating: number;
  view_count: number; clone_count: number;
  cloned_from_id?: string; guest_count: number;
  tags: string[];
  budget_category: "BUDGET" | "MODERATE" | "LUXURY";
  created_at: string; activities?: Activity[];
};

export type GenerateRequest = {
  destination: string; start_date: string; end_date: string;
  budget_vnd: number; guest_count?: number;
  preference_tags?: string[];
  travel_style?: "relaxed" | "standard" | "active";
  travel_month?: number; arrival_time?: string; departure_time?: string;
};

// GenerateResponse: xem chi tiết backend/internal/planner/types.go
export type SlotPlace = {
  id: string; name: string; category: string;
  area?: string; lat: number; lng: number;
  cover_image?: string; images?: string[];
  base_price: number; duration_min: number; hours?: string;
  best_time_of_day?: string; is_must_visit: boolean;
  is_full_day: boolean; is_free: boolean; tags?: string[];
};

export type TimeSlot = {
  start: string; end: string; slot_type: string;
  is_buffer: boolean; combo_covered?: boolean;
  place?: SlotPlace;
};

export type DayPlan = {
  day_num: number; date_str: string; day_type: string;
  primary_area: string; travel_min: number; buffer_min: number;
  slots: TimeSlot[];
};

export type GenerateResponse = {
  days: DayPlan[];
  budget_recap: {
    total_budget_vnd: number; attraction_spent_vnd: number;
    food_spent_vnd: number; remaining_vnd: number; within_budget: boolean;
  };
  combo_result?: { use_combo: boolean; name?: string; savings_vnd?: number; savings_pct?: number };
  violations?: { rule: string; severity: "error" | "warning"; message: string; day?: number }[];
  budget_tier: "survival" | "budget" | "standard" | "premium";
  budget_warning?: string;
  slot_template: "relaxed" | "standard" | "active";
  price_stale_warnings?: string[];
};

export type ChatMessage = {
  role: "user" | "assistant";
  content: string;
  tool_calls?: string[];
  plan?: GenerateResponse | null;
  created_at: string;
};
```

## 7. Error handling chuẩn

```ts
import { toast } from "sonner";
import { ApiError } from "@/lib/api";

export function handleError(e: unknown, fallback = "Có lỗi xảy ra") {
  if (e instanceof ApiError) {
    if (e.status === 429) toast.error("Bạn thao tác quá nhanh. Thử lại sau.");
    else if (e.status === 403) toast.error("Bạn không có quyền thực hiện hành động này.");
    else if (e.status === 404) toast.error("Không tìm thấy dữ liệu.");
    else toast.error(e.message || fallback);
    return;
  }
  toast.error(fallback);
  console.error(e);
}
```

## 8. Folder gợi ý

```
frontend/
├─ app/                  (đã có pages)
├─ components/
│  ├─ require-auth.tsx
│  ├─ place-picker.tsx
│  ├─ plan-preview-card.tsx
│  ├─ chat-message.tsx
│  ├─ chat-input.tsx
│  └─ presence-stack.tsx
├─ hooks/
│  ├─ use-auth.tsx
│  ├─ use-itinerary-ws.ts
│  └─ use-saved-places.ts
└─ lib/
   ├─ api.ts
   ├─ stream-chat.ts
   ├─ plan-to-itinerary.ts
   ├─ tool-labels.ts
   └─ types.ts
```

## 9. Checklist trước khi merge

- [ ] Mọi `fetch` thay bằng `apiFetch`.
- [ ] Mọi page protected bọc `<RequireAuth>` (server middleware tốt hơn).
- [ ] `AuthProvider` ở root layout.
- [ ] WS hook chỉ chạy ở client component, có cleanup khi unmount.
- [ ] SSE: cancel khi user gửi message mới (AbortController).
- [ ] Toast cho mọi action user (save, delete, publish, ...).
- [ ] Loading skeleton cho list/detail page.
- [ ] Empty state có CTA dẫn user.
- [ ] Disabled button trong khi mutate (`isPending`).
- [ ] `cover_image_url` fallback placeholder khi null.
- [ ] Date format hiển thị bằng `date-fns` locale `vi`.
- [ ] Tiền tệ format: `Intl.NumberFormat("vi-VN", { style: "currency", currency: "VND" })`.
