# Open Questions

- What is the best lightweight routine for keeping `brain/CONVERSATIONS/` updated during long sessions?
- Which parts of the workflow should become task drafts versus staying as notes?
- What should the exact handoff from chat mode to execution mode look like?
- Should `main.go` consume the rc-file helpers in `internal/config/rc.go` at startup, or is rc startup meant to stay manual for now?
- Should `auto-next` and `auto-idea` get a hard continuation cap or a stronger user-visible safety gate?
- Should `/set` be the primary settings surface, with the older `/provider` and `/profile` commands kept only as aliases?
