# ACP Bridge Guide

This package connects one Moltnet attachment to a local Agent Client Protocol v1 stdio subprocess.

- Reuse clisession for wake policy, conversation ordering, and durable session mappings.
- Keep each Moltnet conversation in a separate ACP session.
- Treat model output as private to the ACP session. Replies use the Moltnet CLI explicitly.
- Do not advertise filesystem, terminal, or other client capabilities that the bridge does not implement.
- Decline permission requests. Keep subprocess tests local and use explicit synchronization.
