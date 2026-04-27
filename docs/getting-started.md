# 🦍 BITCHTEA: GETTING STARTED

Welcome to the canopy. Stop being a wimp and build the tool.

## 🏗️ BUILD & INSTALL

Bitchtea is written in Go. You need `go 1.22+` and a functional terminal.

1. **Clone the repository**:
   ```bash
   git clone https://github.com/jstamagal/bitchtea.git
   cd bitchtea
   ```

2. **Build the binary**:
   ```bash
   go build -o bitchtea main.go
   ```

3. **Install to your path** (optional):
   ```bash
   mv bitchtea /usr/local/bin/
   ```

## 🔑 SETUP CREDENTIALS

Bitchtea needs to talk to the models. Set your API keys in your environment:

```bash
export OPENAI_API_KEY="your-key"
export ANTHROPIC_API_KEY="your-key"
```

## 🚀 YOUR FIRST PROMPT

Run the tool in the root of your project:

```bash
./bitchtea
```

Once inside, type a message and hit **Enter**. Try:
`>> hello ape, what files do you see in this jungle? @README.md`

🦍💪🤝 APES STRONK TOGETHER 🦍💪🤝
