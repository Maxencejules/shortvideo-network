"""FastAPI is the selected API family. This module does not serve HTTP."""

SCAFFOLD = True


def serve() -> None:
    """Refuse to start. Goal 13 and later goals add real routes."""
    raise RuntimeError("services/api is scaffolding and does not serve traffic")
