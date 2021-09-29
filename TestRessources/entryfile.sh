#!/bin/bash
i=0
while true 
do  
  i=$((i+1))
  echo "Nouvelle ligne log $i pour $HOSTNAME"
  echo "
2021-09-29 11:39:26,769 INFO [org.jasig.cas.services.DefaultServicesManagerImpl] - <Loaded 1 services.>
2021-09-29 11:41:26,768 INFO [org.jasig.cas.services.DefaultServicesManagerImpl] - <Reloading registered services.>
2021-09-29 11:41:26,768 INFO [org.jasig.cas.services.DefaultServicesManagerImpl] - <Loaded 1 services.>
2021-09-29 11:41:38,200 INFO [org.jasig.cas.CentralAuthenticationServiceImpl] - <Granted service ticket [ST-50-rJAZTauzODlBCxj6MACc-sadirah_cas] for service [https://sadirahbdi.pfv.private.sfr.com/sadirah/j_spring_cas_security_check] for user [u163153]>
2021-09-29 11:41:38,202 INFO [com.github.inspektr.audit.support.Slf4jLoggingAuditTrailManager] - <Audit trail record BEGIN
=============================================================
WHO: u163153
WHAT: ST-50-rJAZTauzODlBCxj6MACc-sadirah_cas for https://sadirahbdi.pfv.private.sfr.com/sadirah/j_spring_cas_security_check
ACTION: SERVICE_TICKET_CREATED
APPLICATION: CAS
WHEN: Wed Sep 29 11:41:38 CEST 2021
CLIENT IP ADDRESS: 10.139.20.0
SERVER IP ADDRESS: 10.139.10.185
=============================================================

>
2021-09-29 11:41:38,300 INFO [com.github.inspektr.audit.support.Slf4jLoggingAuditTrailManager] - <Audit trail record BEGIN
=============================================================
WHO: audit:unknown
WHAT: ST-50-rJAZTauzODlBCxj6MACc-sadirah_cas
ACTION: SERVICE_TICKET_VALIDATED
APPLICATION: CAS
WHEN: Wed Sep 29 11:41:38 CEST 2021
CLIENT IP ADDRESS: 10.139.21.36
SERVER IP ADDRESS: 10.139.10.185
=============================================================

>
2021-09-29 11:43:26,768 INFO [org.jasig.cas.services.DefaultServicesManagerImpl] - <Reloading registered services.>
2021-09-29 11:43:26,769 INFO [org.jasig.cas.services.DefaultServicesManagerImpl] - <Loaded 1 services.>
2021-09-29 11:45:26,769 INFO [org.jasig.cas.services.DefaultServicesManagerImpl] - <Reloading registered services.
"
  sleep 10  
done