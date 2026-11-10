"""HTTPS playback of a ready-for-review package. Importing this does not listen.

P2P, TURN, WebRTC, and public IPFS are disabled. The origin adapter is a
loopback test host. It does not publish and it does not write a hosting lease.
"""

from svn_delivery.ids import ADAPTER_NAME, ADAPTER_VERSION, DISABLED

SCAFFOLD = False

__all__ = ["ADAPTER_NAME", "ADAPTER_VERSION", "DISABLED", "SCAFFOLD"]
