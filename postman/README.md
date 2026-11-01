# JanoLuck Postman collection

Files:

- `JanoLuck.postman_collection.json` — all routes, no scripts, no collection variables.
- `JanoLuck.postman_environment.json` — one variable: `bash_url`.

## Import

1. In Postman: **Import** → select both JSON files.
2. Select the **JanoLuck Local** environment.
3. Start the server: `go run ./cmd/server/main.go`.

## Simple usage

1. Run **Auth → Login** (or sign up and verify the email first).
2. Copy `data.accessToken` from the response.
3. Open the collection → **Authorization** → replace `PASTE_ACCESS_TOKEN_HERE` with the token.
4. Replace `PASTE_SESSION_ID_HERE` in session/chat URLs with a real session UUID.
5. Replace `PASTE_REFRESH_TOKEN_HERE` in **Refresh tokens** with the refresh token from Login.
6. Replace `123456` with the real code from the email in **Verify email** / **Reset password**.

## Hardcoded values

- Email: `mdarkanurl@gmail.com`
- Password: `arkan1020`
- Change-password example: `arkan1020` → `arkan1021` (server requires the new password to differ)
