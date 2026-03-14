#!/bin/bash

export POSTGRES_HOST=localhost 
export POSTGRES_PORT=5432
export POSTGRES_DATABASE=prism
export POSTGRES_USER=prism
export POSTGRES_PASSWORD=$(cat .secrets/pg-prism)

gemini