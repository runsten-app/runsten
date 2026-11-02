# Fixtures

`grpc_battery_response.bin` and `grpc_target_soc_response.bin` are responses of the Polestar
app's gRPC services, `GetLatestBattery` and `GetTargetSoc`, in protobuf's wire format: captured
on a real account and anonymized (VIN `YSMYKEAE7RB000000`, IDs of zeros, timestamps at
2024-01-01) by pypolestar's `tests/capture_grpc_fixtures.py`, and copied from
https://github.com/pypolestar/pypolestar (`tests/data/`, MIT License, Copyright (c) 2023 Tuen
Lee).
