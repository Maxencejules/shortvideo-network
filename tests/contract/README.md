# tests/contract

Contract checks are the Goal 01 and Goal 02 checkers:

```
python3 docs/evidence/goal-01/check_requirements.py
python3 docs/evidence/goal-02/check_protocol.py
```

`make contract` runs both. This directory has no pytest files. An empty pytest collection is not a passing contract run; the make target is the entry point, and each checker exits non-zero when its assertions fail.
