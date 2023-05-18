#!/bin/bash

cd $(dirname $0)/..


EXEC="docker buildx"

USER="arielszekely"

TAG="latest"

# ENTER THE ROOT FOLDER
cd ../
ROOT_FOLDER=$(pwd)
$EXEC create --name mysocialnetworkbuilder --use

for i in socialnetworkk8s #frontend geo profile rate recommendation reserve search user #uncomment to build multiple images
do
  IMAGE=${i}
  echo Processing image ${IMAGE}
  cd $ROOT_FOLDER
  $EXEC build -t "$USER"/"$IMAGE":"$TAG" -f Dockerfile --progress=plain . --push #--platform linux/arm64,linux/amd64 
  cd $ROOT_FOLDER
  echo
done


cd - >/dev/null
