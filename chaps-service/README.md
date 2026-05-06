### Summary of the API Layout
| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/chaps` | Initiate a new high-value CHAPS payment. Requires idempotency key and digital signature. | In development |
| **GET** | `/v1/payments/chaps/{id}` | Retrieve the real-time settlement status, ISO 20022 details, and audit trail of a specific payment. | Not implemented |
| **GET** | `/v1/payments/chaps` | List and filter historical CHAPS payments by date range, status, or amount. | Not implemented |
| **POST** | `/v1/payments/chaps/validate` | Perform dry-run validation: checks BIC/IBAN formatting, sanction screening, and liquidity availability. | Not implemented |
| **DELETE** | `/v1/payments/chaps/{id}` | Attempt to cancel a payment. Only available if the status is PENDING and it hasn't reached RTGS. | Not implemented |
| **POST** | `/v1/payments/chaps/{id}/amend` | Amend non-financial details (e.g., remittance info) of a pending payment. | Not implemented |
| **GET** | `/v1/payments/chaps/limits` | Retrieve current clearing limits and remaining intraday liquidity. | Not implemented |
