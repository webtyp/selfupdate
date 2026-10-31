# Apply flow

What the supervisor does on `POST /apply`. Every branch has a test in `tests/apply_test.go`.

```mermaid
flowchart TD
    A[Apply the available version] --> B{Database dump taken?}
    B -->|no| X1[Stop: nothing changed]
    B -->|yes| C{Image pulled<br/>at its digest?}
    C -->|no| X2[Stop: nothing changed]
    C -->|yes| L[Keep the current compose.yaml as<br/>compose.yaml.prev, write the new one]
    L --> M{New version's migration<br/>ran with the owner user?}
    M -->|no| N[Restore compose.yaml<br/>from compose.yaml.prev]
    N --> X3[Stop: nothing switched<br/>version marked as failed]
    M -->|yes| D[Write APP_IMAGE in .env<br/>recreate the app service]
    D --> E{App healthy<br/>within the limit?}
    E -->|yes| F[New version is current<br/>previous one remembered]
    E -->|no| O[Restore compose.yaml<br/>from compose.yaml.prev]
    O --> G[Write the previous APP_IMAGE<br/>recreate the app service]
    G --> H[Mark the version as failed<br/>phase rolled_back]
```
