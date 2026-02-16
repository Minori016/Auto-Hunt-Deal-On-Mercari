#!/bin/bash

# AutoBot Android/Linux Setup Script
# This script guides you through setting up AutoBot on Termux or any Linux system.

set -e

echo "🤖 AutoBot Setup Wizard"
echo "======================="

# 1. Install Dependencies (Termux specific)
if command -v pkg &> /dev/null; then
    echo "📦 Installing dependencies via pkg..."
    pkg update -y
    pkg install golang git jq -y
fi

# 2. Check for Go
if ! command -v go &> /dev/null; then
    echo "❌ Go is not installed. Please install Go 1.20 or later."
    exit 1
fi

# 3. Get Configuration from User
echo ""
echo "📱 Please enter your credentials:"
read -p "1. Telegram Bot Token: " bot_token
read -p "2. Telegram Chat ID: " chat_id
read -p "3. HuggingFace API Key: " hf_key

# 4. Generate config.json using example as template
if [ ! -f "config.json.example" ]; then
    echo "❌ config.json.example not found. Please run this script from the AutoBot root directory."
    exit 1
fi

echo "⚙️ Generating config.json..."
cat config.json.example | jq \
    --arg token "$bot_token" \
    --arg chat "$chat_id" \
    --arg hf "$hf_key" \
    '.telegram.bot_token = $token | .telegram.chat_id = $chat | .huggingface.api_key = $hf | .enable_ai_filter = true | .scan_interval_minutes = 2' \
    > config.json

# 5. Build
echo "🔨 Building AutoBot..."
go build -o autobot ./cmd/autobot/

echo "======================="
echo "✅ Setup complete!"
echo "🚀 To start your bot, run: ./autobot"
echo "======================="
