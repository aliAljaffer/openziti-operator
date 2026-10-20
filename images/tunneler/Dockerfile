ARG ZITI_VERSION=2.0.4
FROM openziti/ziti-router:${ZITI_VERSION}
USER 0
RUN setcap cap_net_admin,cap_net_raw+eip /usr/local/bin/ziti \
 && setcap cap_net_admin,cap_net_raw+eip "$(readlink -f /usr/sbin/iptables)"
USER 2171
