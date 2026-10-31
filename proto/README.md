# proto

Frozen protocol sources stay in `docs/protocol/proto`. This directory holds generated code and the small handwritten test that links it.

- `proto/svn/` is generated Go. `scripts/gen_proto.sh` writes it. Do not edit those files by hand.
- `proto/gen/python/` is generated Python. The `__init__.py` files are handwritten so the packages import.
- `proto/check/` is a handwritten test of one generated enum.
- `proto/tools/` pins `protoc-gen-go` and is not part of the test binary.

`option go_package` in the frozen protos selects the module path `github.com/Maxencejules/shortvideo-network/protocol`. Goal 03 does not change those options.
