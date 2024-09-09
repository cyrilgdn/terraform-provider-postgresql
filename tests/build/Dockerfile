FROM postgres:${PGVERSION:-latest}

RUN apt-get update && apt-get install -y build-essential postgresql-server-dev-all
COPY dummy_seclabel /opt/dummy_seclabel
WORKDIR /opt/dummy_seclabel
RUN make
