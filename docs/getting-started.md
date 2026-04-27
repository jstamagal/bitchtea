# 🦍 GETTING STARTED 🦍

Welcome to the canopy! Follow these steps to start coding with `bitchtea`.

## 1. Build and Install

You need **Go 1.26** or later.

```bash
# Clone the repo
git clone https://github.com/jstamagal/bitchtea.git
cd bitchtea

# Build the binary
go build -o bitchtea .

# Move it to your path
sudo mv bitchtea /usr/local/bin/
```

## 2. Setup API Keys

`bitchtea` needs to talk to an LLM. Set your key in your shell profile (`.bashrc` or `.zshrc`):

```bash
export OPENAI_API_KEY="sk-..."
# OR
export ANTHROPIC_API_KEY="sk-ant-..."
```

## 3. Your First Prompt

1. Launch the app: `bitchtea`
2. Type a message: `Hello! What files are in this directory?`
3. Press **Enter**.
4. Watch the agent think, run tools, and reply.
5. Use `/quit` when you are done.

APE STRONK TOGETHER. 🦍💪🤝
