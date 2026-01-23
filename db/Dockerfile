FROM postgres:18-trixie
RUN apt-get update && \
  apt-get -y upgrade && \
  apt-get -y install postgresql-18-cron && \
  apt-get clean \
  && rm -rf /var/lib/apt/lists/*