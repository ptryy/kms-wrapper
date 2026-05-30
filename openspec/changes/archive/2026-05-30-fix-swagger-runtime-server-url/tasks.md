## 1. Runtime swagger server URL handling

- [x] 1.1 Add gateway-side logic to serve `/swagger/doc.json` with a server URL derived from the incoming request origin instead of fixed localhost:8080
- [x] 1.2 Keep existing swagger enable/auth behavior unchanged while wiring the runtime doc handler

## 2. Validation and regression coverage

- [x] 2.1 Add/extend gateway tests to assert served `/swagger/doc.json` uses the active request host/port and not localhost:8080 on custom ports
- [x] 2.2 Regenerate swagger artifacts if required and run repo validation commands
