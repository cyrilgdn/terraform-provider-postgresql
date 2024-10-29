ARG PGVERSION
FROM postgres:${PGVERSION:-latest}

ARG PGVERSION
RUN apt-get update && apt-get install -y build-essential postgresql-server-dev-${PGVERSION:-all}
RUN dpkg -l |grep postgresql
COPY dummy_seclabel /opt/dummy_seclabel
WORKDIR /opt/dummy_seclabel
RUN make
