#ifndef _WRAPPER_H_
#define _WRAPPER_H_

#include <stdlib.h>
#include <stdio.h>
#include <xpc/xpc.h>

extern xpc_type_t TYPE_ERROR;

extern xpc_type_t TYPE_ARRAY;
extern xpc_type_t TYPE_DATA;
extern xpc_type_t TYPE_DICT;
extern xpc_type_t TYPE_INT64;
extern xpc_type_t TYPE_STRING;

extern xpc_object_t ERROR_CONNECTION_INVALID;
extern xpc_object_t ERROR_CONNECTION_INTERRUPTED;
extern xpc_object_t ERROR_CONNECTION_TERMINATED;

extern xpc_connection_t XpcConnectBlued(void *);
extern void XpcSendMessage(xpc_connection_t, xpc_object_t, bool);
extern void XpcArrayApply(void *, xpc_object_t);
extern void XpcDictApply(void *, xpc_object_t);

#endif