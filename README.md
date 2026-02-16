# 🤖 Mercari AutoBot

[![Go Version](https://img.shields.io/badge/Go-1.20+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20RaspberryPi-orange)](https://github.com/Minori016/Auto-Hunt-Deal-On-Mercari)

**AutoBot** is an ultra-lightweight, high-performance automated deal hunter for **Mercari Japan**. It monitors specific luxury brands, designer labels, and vintage sportswear, utilizing AI to filter out "trash" listings and alerting you instantly via Telegram.

Inspired by the efficiency of [PicoClaw](https://github.com/sipeed/picoclaw), it is designed to run 24/7 on low-resource hardware like a **Raspberry Pi** with minimal RAM usage (~30MB).

---

## ✨ Features

- **🔍 Smart Scanning**: Uses Mercari's internal API with built-in **DPoP JWT Authentication** (ES256) to ensure reliable access.
- **🤖 AI-Powered Filtering**: Integrates HuggingFace **CLIP** (Zero-shot Image Classification) to automatically reject listings of empty boxes, shopping bags, receipts, and blurry photos.
- **📱 Instant Alerts**: Rich Telegram notifications including product photos, formatted prices, brand tags, and direct one-click links to Mercari.
- **💾 Dual-Layer Deduplication**: Uses a local **SQLite** database to track seen items, ensuring you never receive the same deal twice.
- **⚙️ Deep Configuration**: Highly customizable brand lists, per-brand price overrides, keyword matching, and adjustable scan intervals.
- **🪶 Optimized for RPi**: Written in Go for maximum efficiency. No headless browsers or heavy dependencies required.
- **🛡️ Robustness**: Built-in panic recovery and exponential backoff for network retries to ensure 24/7 uptime.

---

### Quick Android Setup
1. Download **[Termux](https://f-droid.org/repo/com.termux_0.118.apk)** and open it.
2. Copy and paste this single line to start the guided setup:
   ```bash
   pkg install git -y && git clone https://github.com/Minori016/Auto-Hunt-Deal-On-Mercari.git && cd Auto-Hunt-Deal-On-Mercari && chmod +x setup.sh && ./setup.sh
   ```
3. Follow the on-screen instructions to enter your Token and Chat ID.

### 3. Configuration
Copy the example config and edit it with your credentials and brands:
```bash
cp config.json.example config.json
# Open config.json and fill in your details
```

### 4. Running
```bash
# Start the bot (normal mode)
go run ./cmd/autobot/

# Run a single scan cycle to test
go run ./cmd/autobot/ --once

# Test Telegram connection
go run ./cmd/autobot/ --test-telegram
```

---

## 🍓 Raspberry Pi Deployment

AutoBot is perfect for a Raspberry Pi Zero, 3, 4, or 5.

### Cross-Compilation
Build the binary on your PC for the Pi:
```bash
# For 64-bit Pi OS (Recommended)
GOOS=linux GOARCH=arm64 go build -o autobot-pi ./cmd/autobot/

# For 32-bit Pi OS
GOOS=linux GOARCH=arm go build -o autobot-pi ./cmd/autobot/
```

### Run as a Systemd Service
To keep the bot running 24/7, create a service file: `/etc/systemd/system/autobot.service`
```ini
[Unit]
Description=Mercari AutoBot Service
After=network.target

[Service]
Type=simple
User=pi
WorkingDirectory=/home/pi/autobot
ExecStart=/home/pi/autobot/autobot-pi
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```
Then start it: `systemctl enable --now autobot`

---

## 🛠 Project Structure

```text
├── cmd/autobot/          # Main entrypoint & CLI logic
├── pkg/
│   ├── mercari/         # Mercari API, DPoP, & AI Filter
│   ├── telegram/        # Telegram Notifier
│   └── store/           # SQLite Dedup Store
├── config/              # Configuration loader
├── .gitignore           # Safe for GitHub
├── config.json.example  # Template for users
└── Start-AutoBot.bat    # One-click Windows starter
```

---

## 🤝 Contributing
Feel free to open issues or submit pull requests for new features, better filters, or anti-bot bypasses.

## ⚖️ Disclaimer
This tool is for educational and personal use only. Use at your own risk. Respect Mercari's Terms of Service and API rate limits.

---
*Created with ❤️ by [Minori016](https://github.com/Minori016)*
