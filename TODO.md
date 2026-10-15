# TODO

## Future Considerations

- **Pin reusable workflow versions on callers**:
  - Transition caller repositories from tracking relaxed revisions (`@main`) to pinned releases or tags to reduce supply chain risks when reusable workflows change.
- **Evaluate Socket.dev integration**:
  - Evaluate adding Socket.dev (Socket for GitHub) to catch unexpected upstream behavior (eval, install scripts, network access) in dependency updates, while balancing the risk of granting permissions to an external GitHub App.
