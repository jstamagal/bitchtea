  while [ -f DELETE_WHEN_CANT_GO_ON ]; do
    printf '%s\n' \
      'Pick one concrete issue from `bd ready` and complete it end to
  end.' \
      'Rules:' \
      '- Work only in /home/admin/playground/bitchtea' \
      '- Prefer a leaf task, not a broad parent issue' \
      '- Claim the issue before coding' \
      '- Run required checks before closing it' \
      '- Commit, push git, run `bd dolt push`, and close the issue when
  done' \
      '- bd is /home/linuxbrew/.linuxbrew/bin/bd' \
      '- If blocked, create or update the relevant beads issue with the
  blocker, then stop' \
      '- If no ready task is suitable, stop' \
      | codex exec
  done

  mpg123 ~/crab_rave.mp3

