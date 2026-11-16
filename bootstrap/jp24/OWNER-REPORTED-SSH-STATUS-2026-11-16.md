# Owner-reported persistent SSH status

Date: 2026-11-16

The owner reports that work continued with Claude and successfully established persistent SSH access to the PRIME 4 G2 using a reverse shell.

This is a direct owner report. The Claude implementation, exact mechanism, boot persistence behavior, connection path, and repeatability have not yet been independently reviewed or added to this repository. Treat it as the current reported hardware outcome, separate from the earlier `/data` image/ALSA-hook experiment.

Given the reported working access, another `/data` flash is not currently needed to regain SSH.

Before incorporating the implementation into this project, import the relevant source and a sanitized session summary on a dedicated branch. Document boot and reconnect steps and evidence of repeatability. Do not commit private keys, credentials, public endpoints, or tunnel secrets.
