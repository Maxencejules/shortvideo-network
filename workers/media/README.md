# workers/media

Scaffolding for isolated media jobs. Goal 04 pins the development-profile FFmpeg package. Goal 13 leaves a `QUEUED` row in `api.processing_jobs` and the input under a generated name in private quarantine. Goal 14 selects the isolated worker build, records its licence flags, and runs it outside the API process. This package does not invoke a shell.
