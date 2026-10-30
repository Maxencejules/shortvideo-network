GOAL 14 / 30



Isolated video processing workers
  GPT-6.1 Sol                                                                        EFFORT: HIGH

Prerequisites: Goal 13; approved media/package formats and runnable worker environment.
Blueprint reference: pp. 5, 14, 16, 19 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 14. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Build bounded FFmpeg processing that produces checked playback assets without exposing validators or
application secrets.
IMPLEMENTATION REQUIREMENTS

1. Implement workers/media jobs using Celery/RabbitMQ or the approved queue adapter. Probe the actual
    input, enforce duration/dimensions/resource budgets and normalize orientation. Produce 480p/720p
    outputs when appropriate without upscaling, plus thumbnails and reviewed/user-provided captions.
2. Execute FFmpeg with argument arrays, generated paths and narrowly allowed protocols. Use an
    unprivileged isolated runtime, disposable workspace, bounded CPU/memory/disk/wall time and no
    unnecessary outbound network. Do not run untrusted media beside signer keys.
3. Create versioned manifests with segment/rendition metadata, hashes and byte totals. Validate outputs,
    synchronization and allowed formats. A new encoding creates a new package version; do not require
    separate encoders to emit identical bytes for consensus.
4. Make processing retry-safe with staging paths, atomic ready-for-review transitions and cleanup on
    cancel/crash. Reject excessive output growth. Preserve failure reasons without publishing stack traces,
    paths or secrets to users.
5. Record the exact FFmpeg build, codec configuration and licence notices. Where runtime isolation cannot be
    exercised, report that security boundary as unverified and retain an external test profile.
ACCEPTANCE CHECKS

Process valid portrait, rotated, low-resolution and audio/video fixtures; test malformed, truncated and
over-budget inputs. Verify captions, duration, hashes and total bytes.
Kill a worker mid-job, restart and confirm one accepted package without leaked files/quota or accidental
publication. Check network/process/resource restrictions directly where permitted.
Measure processing time on declared hardware; the proposed 120-second target for a 60-second input is a
test gate, not an assumed result.
REQUIRED HANDOFF

Provide runnable workers, small permitted fixtures, sandbox evidence and build/licence records. Hand off
verified package assets to delivery and provider services.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                 20 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                 CODEX CLOUD + GITHUB



