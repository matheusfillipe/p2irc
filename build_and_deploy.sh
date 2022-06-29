#!/bin/bash

DEPLOY_PATH=/var/www/sendirc

sudo go build -o $DEPLOY_PATH/index.x . 
sudo cp index.html $DEPLOY_PATH/index.html

