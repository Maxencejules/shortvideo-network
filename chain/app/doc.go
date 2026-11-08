package app

// Scaffold reports that a module slot is still a placeholder. x/authority
// (Goal 06), x/providers and x/content (Goal 07), x/leases (Goal 08),
// x/evidence (Goal 09), and x/policy (Goal 10) are real modules. The svn
// header slot stays scaffolding (decision request DR-10-28): it stores the
// genesis header and the consensus params and has no message or query. The
// binary, genesis, and four-validator devnet are implemented.
const Scaffold = true
