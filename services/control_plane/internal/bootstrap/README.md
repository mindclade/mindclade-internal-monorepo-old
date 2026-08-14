# Go control-plane bootstrap

Every Go control-plane executable follows the same path:

```text
role -> ProfileFor -> provider Factory -> typed foundation Dependencies
     -> capability validation -> servicekit.Assembly -> RunWithSignals
```

The assembly order is fixed:

```text
foundation -> infrastructure -> coordination -> work -> serving
```

This package owns process composition only. Domain policy remains under
`control/`; provider construction remains under `services/control_plane/internal/providers`;
transport stacks remain under `services/control_plane/internal/transport`.
