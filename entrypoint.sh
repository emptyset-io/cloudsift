#!/bin/sh
# Ensure group-based access to AWS credentials
AWS_GID=$(stat -c "%g" /home/cloudsift/.aws)   # Get GID of the mounted directory
addgroup -g $AWS_GID aws 2>/dev/null || true   # Create the group (if doesn't exist)
adduser cloudsift aws 2>/dev/null || true      # Add cloudsift to the group (if not already)

# Now run the main command
exec "$@"
