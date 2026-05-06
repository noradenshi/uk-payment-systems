### Summary of the API Layout
| Method | Endpoint | Description | Status |
| :--- | :--- | :--- | :--- |
| **POST** | `/v1/payments/chaps` | Initiate a new high-value CHAPS payment. Requires idempotency key and digital signature. | In development |
| **POST** | `/v1/payments/chaps/{id}/authorize` | Second-factor approval required for high-value release. | Not implemented |
| **GET** | `/v1/payments/chaps/{id}` | Retrieve the real-time settlement status, ISO 20022 details, and audit trail of a specific payment. | Not implemented |
| **GET** | `/v1/payments/chaps` | List and filter historical CHAPS payments by date range, status, or amount. | Not implemented |
| **POST** | `/v1/payments/chaps/validate` | Perform dry-run validation: checks BIC/IBAN formatting, sanction screening, and liquidity availability. | Not implemented |
| **DELETE** | `/v1/payments/chaps/{id}` | Attempt to cancel a payment. Only available if the status is PENDING and it hasn't reached RTGS. | Not implemented |
| **POST** | `/v1/payments/chaps/{id}/amend` | Amend non-financial details (e.g., remittance info) of a pending payment. | Not implemented |
| **GET** | `/v1/payments/chaps/limits` | Retrieve current clearing limits and remaining intraday liquidity. | Not implemented |
| **POST** | `/v1/participants/register` | Initial onboarding of a bank's BIC and settlement account. | Not implemented |
| **PATCH** | `/v1/participants/{bic}/status` | Updates visibility (e.g., `ACTIVE`, `SUSPENDED`, `DISABLED`). | Not implemented |
| **POST** | `/v1/participants/{bic}/block` | Immediate "Kill-switch." Halts all outbound settlement instructions. | Not implemented |
| **GET** | `/v1/participants/{bic}/block` | Returns details: who blocked the bank, when, and the reason (e.g., `FRAUD_SUSPECTED`). | Not implemented |
| **DELETE** | `/v1/participants/{bic}/block` | Unblocks the participant and restores settlement rights. | Not implemented |
| **GET** | `/v1/participants/{bic}/positions` | Real-time view of `Earmarked` vs. `Available` liquidity. | Not implemented |
| **GET** | `/v1/system/schedule` | Returns today's cut-off times (which can change on bank holidays). | Not implemented |
| **POST** | `/v1/liquidity/top-up` | **Simulation Only:** Injects funds into the bank's settlement account. | Not implemented |
