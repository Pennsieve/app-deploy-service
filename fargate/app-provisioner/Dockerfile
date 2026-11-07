FROM debian:bookworm

WORKDIR /usr/src/app

RUN  apt-get update && apt-get -y install wget && apt-get -y install gpg && apt-get install -y lsb-release && apt-get install -y curl && apt-get install -y unzip

# install Go
RUN wget https://go.dev/dl/go1.27.2.linux-amd64.tar.gz && \
  echo "ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5  go1.27.2.linux-amd64.tar.gz" | sha256sum -c -

RUN  rm -rf /usr/local/go && tar -C /usr/local -xzf go1.27.2.linux-amd64.tar.gz

ENV PATH="${PATH}:/usr/local/go/bin"

# cleanup
RUN rm -f go1.27.2.linux-amd64.tar.gz

COPY . ./
RUN go mod tidy

RUN go build -v -o /usr/local/bin/app .


CMD [ "app" ]