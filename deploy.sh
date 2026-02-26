#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

APP_NAME="ortodoxa-gudstjanster"
REGION="arn"

# Check if flyctl is installed
if ! command -v flyctl &> /dev/null; then
    echo "Error: flyctl is not installed. Install it with:"
    echo "  curl -L https://fly.io/install.sh | sh"
    exit 1
fi

# Check if authenticated
if ! flyctl auth whoami &> /dev/null; then
    echo "Not authenticated with Fly.io. Logging in..."
    flyctl auth login
fi

# Check if app already exists
if flyctl apps list | grep -q "$APP_NAME"; then
    echo "App $APP_NAME already exists. Deploying update..."
    flyctl deploy --app "$APP_NAME"
else
    echo "Creating new app: $APP_NAME"

    # Create the app
    flyctl apps create "$APP_NAME" --machines

    # Create persistent volume for data storage
    echo "Creating persistent volume..."
    flyctl volumes create church_services_data --region "$REGION" --size 1 --app "$APP_NAME" -y

    # Set secrets
    if [ -f "gitignore/apikey.txt" ]; then
        echo "Setting OPENAI_API_KEY secret..."
        flyctl secrets set OPENAI_API_KEY="$(cat gitignore/apikey.txt)" --app "$APP_NAME"
    else
        echo "Warning: gitignore/apikey.txt not found. Set the secret manually with:"
        echo "  flyctl secrets set OPENAI_API_KEY=your-key --app $APP_NAME"
    fi

    # Deploy
    echo "Deploying application..."
    flyctl deploy --app "$APP_NAME"
fi

echo ""
echo "Deployment complete!"
echo "App URL: https://$APP_NAME.fly.dev/"
echo "ICS URL: https://$APP_NAME.fly.dev/calendar.ics"
