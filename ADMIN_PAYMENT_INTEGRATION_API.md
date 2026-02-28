# Sub2API Admin API: Payment Integration

This document describes the minimum Admin API surface for external payment systems (for example, sub2apipay) to complete balance top-up and reconciliation.

## Base URL

- Production: `https://<your-domain>`
- Beta: `http://<your-server-ip>:8084`

All endpoints below use:

- Header: `x-api-key: admin-<64hex>` (recommended for server-to-server)
- Header: `Content-Type: application/json`

Note: Admin JWT is also accepted by admin routes, but machine-to-machine integration should use admin API key.

## 1) Create + Redeem in One Step

`POST /api/v1/admin/redeem-codes/create-and-redeem`

Purpose:

- Atomically create a deterministic redeem code and redeem it to a target user.
- Typical usage: called after payment callback succeeds.

Required headers:

- `x-api-key`
- `Idempotency-Key`

Request body:

```json
{
  "code": "s2p_cm1234567890",
  "type": "balance",
  "value": 100.0,
  "user_id": 123,
  "notes": "sub2apipay order: cm1234567890"
}
```

Rules:

- `code`: external deterministic order-mapped code.
- `type`: currently recommended `balance`.
- `value`: must be `> 0`.
- `user_id`: target user id.

Idempotency semantics:

- Same `code`, same `used_by` user: return `200` (idempotent replay).
- Same `code`, different `used_by` user: return `409` conflict.
- Missing `Idempotency-Key`: return `400` (`IDEMPOTENCY_KEY_REQUIRED`).

Example:

```bash
curl -X POST "${BASE}/api/v1/admin/redeem-codes/create-and-redeem" \
  -H "x-api-key: ${KEY}" \
  -H "Idempotency-Key: pay-cm1234567890-success" \
  -H "Content-Type: application/json" \
  -d '{
    "code":"s2p_cm1234567890",
    "type":"balance",
    "value":100.00,
    "user_id":123,
    "notes":"sub2apipay order: cm1234567890"
  }'
```

## 2) Query User (Optional Pre-check)

`GET /api/v1/admin/users/:id`

Purpose:

- Check whether target user exists before payment finalize/retry.

Example:

```bash
curl -s "${BASE}/api/v1/admin/users/123" \
  -H "x-api-key: ${KEY}"
```

## 3) Balance Adjustment (Existing Interface)

`POST /api/v1/admin/users/:id/balance`

Purpose:

- Existing reusable admin interface for manual correction.
- Supports `set`, `add`, `subtract`.

Request body example (`subtract`):

```json
{
  "balance": 100.0,
  "operation": "subtract",
  "notes": "manual correction"
}
```

Example:

```bash
curl -X POST "${BASE}/api/v1/admin/users/123/balance" \
  -H "x-api-key: ${KEY}" \
  -H "Idempotency-Key: balance-subtract-cm1234567890" \
  -H "Content-Type: application/json" \
  -d '{
    "balance":100.00,
    "operation":"subtract",
    "notes":"manual correction"
  }'
```

## 4) Error Handling Recommendations

- Persist upstream payment result independently from recharge result.
- Mark payment success immediately after callback verification.
- If recharge fails after payment success, keep order retryable by admin operation.
- For retry, always reuse deterministic `code` + new `Idempotency-Key`.

## 5) Suggested `doc_url` Setting

Sub2API already supports `doc_url` in system settings.

Recommended values:

- View URL: `https://github.com/Wei-Shaw/sub2api/blob/main/ADMIN_PAYMENT_INTEGRATION_API.md`
- Direct download URL: `https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/ADMIN_PAYMENT_INTEGRATION_API.md`
