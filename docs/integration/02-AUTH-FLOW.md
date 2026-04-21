# 02 — Luồng xác thực

## 1. Đăng ký + Verify email

```
User                Frontend                 Backend                  Email
 │                    │                         │                       │
 │ Submit form        │                         │                       │
 │───────────────────►│ POST /auth/register     │                       │
 │                    │  { email, password,     │                       │
 │                    │    full_name }          │                       │
 │                    │────────────────────────►│                       │
 │                    │                         │ Tạo user (status=     │
 │                    │                         │ UNVERIFIED), gen      │
 │                    │                         │ verify_token          │
 │                    │                         │──────────────────────►│
 │                    │                         │   gửi link verify     │
 │                    │ 201 { user }            │                       │
 │                    │◄────────────────────────│                       │
 │ redirect           │                         │                       │
 │ /auth/verify       │                         │                       │
 │ ?email=...         │                         │                       │
 │                    │                         │                       │
 │ Mở mail → click    │                         │                       │
 │ link có ?token=    │                         │                       │
 │───────────────────►│ POST /auth/verify       │                       │
 │                    │  { token }              │                       │
 │                    │────────────────────────►│ verify, set ACTIVE    │
 │                    │ 200 { message }         │                       │
 │                    │◄────────────────────────│                       │
 │                    │ redirect /auth/login    │                       │
```

**Edge cases**:
- User chưa nhận mail → bấm "Gửi lại" → `POST /auth/resend-verification { email }`. Backend luôn trả 200 (chống email enumeration).
- Token hết hạn → 400, hiện lỗi và nút resend.
- Email đã tồn tại → `POST /auth/register` trả 409.

## 2. Đăng nhập email/password

```
User → Frontend                 Backend
       │ POST /auth/login        │
       │ {email,password}        │
       │────────────────────────►│ verify password, check ACTIVE
       │                         │ sign JWT (sub=user_id, exp=7d)
       │ 200 {token,user}        │
       │◄────────────────────────│
       │
       │ lưu token (cookie httpOnly hoặc localStorage)
       │ context: setUser(user)
       │ redirect /planner hoặc redirect_to
```

**Lưu token**:
- **Khuyến nghị**: cookie `Secure; HttpOnly; SameSite=Lax` set bởi route handler Next.js (proxy login). Tránh XSS.
- Nếu chấp nhận đơn giản: `localStorage.setItem("token", ...)` — phải set CSP nghiêm ngặt.

**JWT claims (backend phát)**: `{ sub: user_id, exp: ..., iat: ... }`. Frontend không cần verify, chỉ gắn vào header.

## 3. OAuth Google

```
Frontend                                    Google         Backend
   │ Render Google Identity button          │              │
   │ (NEXT_PUBLIC_GOOGLE_CLIENT_ID)         │              │
   │ user click ────────────────────────────►│              │
   │                                        │ trả id_token │
   │◄───────────────────────────────────────│              │
   │ POST /auth/google { id_token }                        │
   │──────────────────────────────────────────────────────►│
   │                                                       │ verify với Google,
   │                                                       │ tạo user nếu chưa,
   │                                                       │ phát JWT
   │ 200 { token, user }                                   │
   │◄──────────────────────────────────────────────────────│
```

Facebook tương tự, body `{ access_token }`.

## 4. Auth context phía frontend

`hooks/use-auth.tsx` (cần tạo) cung cấp:

```ts
type AuthCtx = {
  user: User | null;
  token: string | null;
  loading: boolean;
  login: (email: string, pass: string) => Promise<void>;
  loginGoogle: (idToken: string) => Promise<void>;
  logout: () => void;
  refresh: () => Promise<void>;        // gọi /auth/me
};
```

Bootstrap khi app mount:
1. Đọc token từ storage. Nếu không có → `user=null`.
2. Nếu có → `GET /auth/me`. Thành công: setUser. Thất bại 401: clear token + redirect login.

## 5. Bảo vệ route

```
RootLayout
└─ <AuthProvider>
   ├─ Public pages: /, /explore, /places, /combos, /auth/*, /itinerary/[id]/public
   └─ <RequireAuth>          ← redirect /auth/login?redirect=<current> nếu chưa login
        ├─ /planner
        ├─ /profile, /saved, /settings/*
        ├─ /itinerary/[id], /itinerary/new, /itinerary/[id]/edit
        └─ /ai-planner (chat lưu session theo user)
   └─ <RequireAdmin>         ← thêm check role
        └─ /admin/*
```

`RequireAuth` đọc từ context. Trong server component dùng cookie + verify JWT signature ở Next middleware (`middleware.ts`) cho hiệu năng tốt hơn.

## 6. Quản lý lifecycle JWT

| Event | Hành động |
|---|---|
| Login thành công | Lưu token, setUser, redirect. |
| API trả 401 | `lib/api.ts` interceptor: clear token, push `/auth/login?redirect=<from>`, hiện toast "Phiên hết hạn". |
| Đổi mật khẩu | Backend hiện không revoke token. Frontend tự logout sau khi đổi để bắt buộc login lại. |
| Logout | Xoá token + cookie + state, redirect `/`. (Backend hiện không có endpoint logout — JWT stateless.) |

## 7. Đổi mật khẩu

```
Profile/Settings/Security
  POST /user/change-password { old_password, new_password }
  → 200: toast "Đã đổi mật khẩu", logout + redirect login
  → 400: hiện lỗi (mật khẩu cũ sai / mật khẩu mới không hợp lệ)
```

## 8. Validation rules (frontend mirror backend)

- `email`: regex chuẩn + max 255.
- `password`: min 8, có chữ + số. (Tăng strict tuỳ business.)
- `full_name`: 2-100 ký tự.
- OTP/token verify: theo link có sẵn trong email; nếu chuyển sang OTP 6 số, đổi backend trước.
