# NOT-RELEASED local reference. Do not push this image.
# Untrusted media processing. No validator signing material is copied.
# FFmpeg is not installed here: an unpinned download is refused.
# The operator installs Ubuntu package 7:6.1.1-3ubuntu5, recorded in
# docs/operations/ffmpeg-obligations.md.
FROM debian:bookworm-slim@sha256:a3bba7dc7dbcc1f37c2856c51fc9a7fac96608f50cb87b1b4e4d8b8c85ae988e
RUN useradd --create-home --uid 65532 --shell /usr/sbin/nologin svn
USER svn
WORKDIR /home/svn
COPY --chown=65532:65532 NOTICE /home/svn/NOTICE
