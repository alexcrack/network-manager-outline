# Debian packaging (phase 4)

Planned binary packages:
- `network-manager-outline`: nm-outline-service, .name file, D-Bus policy
- `plasma-nm-outline`: Plasma UI plugin. Must depend on the exact plasma-nm
  version it was built against (no stable ABI), e.g. `plasma-nm (= ${plasma-nm:Version})`.
