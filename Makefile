OBJ	=	objs
DEP	=	dep
EXE = ${OBJ}/bin

COMMIT := $(shell git log -1 --pretty=format:"%H")
UNAME_S := $(shell uname -s)
OPENSSL_CFLAGS ?= $(shell pkg-config --cflags openssl 2>/dev/null)
OPENSSL_LIBS ?= $(shell pkg-config --libs openssl 2>/dev/null)
EPOLL_SHIM_CFLAGS ?= $(shell pkg-config --cflags epoll-shim 2>/dev/null)
EPOLL_SHIM_LIBS ?= $(shell pkg-config --libs epoll-shim 2>/dev/null)

ARCH =
ifeq ($m, 32)
ARCH = -m32
endif
ifeq ($m, 64)
ARCH = -m64
endif

COMMONFLAGS = -O3 -std=gnu11 -Wall -Wno-array-bounds -fno-strict-aliasing -fno-strict-overflow -fwrapv -DAES=1 -DCOMMIT=\"${COMMIT}\"

ifeq ($(UNAME_S),Darwin)
    OPENSSL_PREFIX ?= $(shell brew --prefix openssl@3 2>/dev/null || brew --prefix openssl 2>/dev/null)
    ifneq ($(strip $(OPENSSL_PREFIX)),)
        OPENSSL_CFLAGS += -I$(OPENSSL_PREFIX)/include
        OPENSSL_LIBS += -L$(OPENSSL_PREFIX)/lib -Wl,-rpath,$(OPENSSL_PREFIX)/lib
    endif
    ifeq ($(strip $(OPENSSL_LIBS)),)
        OPENSSL_LIBS = -lcrypto
    endif
    CFLAGS = $(ARCH) $(COMMONFLAGS) $(OPENSSL_CFLAGS) $(EPOLL_SHIM_CFLAGS)
    LDFLAGS = $(ARCH) -ggdb -lm $(OPENSSL_LIBS) $(EPOLL_SHIM_LIBS) -lz -lpthread
    AR_TOOL ?= /usr/bin/ar
    RANLIB_TOOL ?= /usr/bin/ranlib
else
    CFLAGS = $(ARCH) $(COMMONFLAGS) -mpclmul -march=core2 -mfpmath=sse -mssse3 -D_GNU_SOURCE=1 -D_FILE_OFFSET_BITS=64
    LDFLAGS = $(ARCH) -ggdb -rdynamic -lm -lrt -lcrypto -lz -lpthread
    AR_TOOL ?= ar
    RANLIB_TOOL ?= ranlib
endif

LIB = ${OBJ}/lib
CINCLUDE = -iquote common -iquote .

LIBLIST = ${LIB}/libkdb.a

PROJECTS = common jobs mtproto net crypto engine

OBJDIRS := ${OBJ} $(addprefix ${OBJ}/,${PROJECTS}) ${EXE} ${LIB}
DEPDIRS := ${DEP} $(addprefix ${DEP}/,${PROJECTS})
ALLDIRS := ${DEPDIRS} ${OBJDIRS}


.PHONY:	all clean go-build go-test go-smoke go-stability go-dualrun go-linux-docker-check

DOCKER_GO_IMAGE ?= golang:bookworm
DOCKER_PLATFORM ?=

EXELIST	:= ${EXE}/mtproto-proxy


OBJECTS	=	\
  ${OBJ}/mtproto/mtproto-proxy.o ${OBJ}/mtproto/mtproto-config.o ${OBJ}/net/net-tcp-rpc-ext-server.o

DEPENDENCE_CXX		:=	$(subst ${OBJ}/,${DEP}/,$(patsubst %.o,%.d,${OBJECTS_CXX}))
DEPENDENCE_STRANGE	:=	$(subst ${OBJ}/,${DEP}/,$(patsubst %.o,%.d,${OBJECTS_STRANGE}))
DEPENDENCE_NORM	:=	$(subst ${OBJ}/,${DEP}/,$(patsubst %.o,%.d,${OBJECTS}))

LIB_OBJS_NORMAL := \
	${OBJ}/common/crc32c.o \
	${OBJ}/common/pid.o \
	${OBJ}/common/sha1.o \
	${OBJ}/common/sha256.o \
	${OBJ}/common/md5.o \
	${OBJ}/common/resolver.o \
	${OBJ}/common/parse-config.o \
	${OBJ}/crypto/aesni256.o \
	${OBJ}/jobs/jobs.o ${OBJ}/common/mp-queue.o \
	${OBJ}/net/net-events.o ${OBJ}/net/net-msg.o ${OBJ}/net/net-msg-buffers.o \
	${OBJ}/net/net-config.o ${OBJ}/net/net-crypto-aes.o ${OBJ}/net/net-crypto-dh.o ${OBJ}/net/net-timers.o \
	${OBJ}/net/net-connections.o \
	${OBJ}/net/net-rpc-targets.o \
	${OBJ}/net/net-tcp-connections.o ${OBJ}/net/net-tcp-rpc-common.o ${OBJ}/net/net-tcp-rpc-client.o ${OBJ}/net/net-tcp-rpc-server.o \
	${OBJ}/net/net-http-server.o \
	${OBJ}/common/tl-parse.o ${OBJ}/common/common-stats.o \
	${OBJ}/engine/engine.o ${OBJ}/engine/engine-signals.o \
	${OBJ}/engine/engine-net.o \
	${OBJ}/engine/engine-rpc.o \
	${OBJ}/engine/engine-rpc-common.o \
	${OBJ}/net/net-thread.o ${OBJ}/net/net-stats.o ${OBJ}/common/proc-stat.o \
	${OBJ}/common/kprintf.o \
	${OBJ}/common/precise-time.o ${OBJ}/common/cpuid.o \
	${OBJ}/common/server-functions.o ${OBJ}/common/crc32.o \

LIB_OBJS := ${LIB_OBJS_NORMAL}

DEPENDENCE_LIB	:=	$(subst ${OBJ}/,${DEP}/,$(patsubst %.o,%.d,${LIB_OBJS}))

DEPENDENCE_ALL		:=	${DEPENDENCE_NORM} ${DEPENDENCE_STRANGE} ${DEPENDENCE_LIB}

OBJECTS_ALL		:=	${OBJECTS} ${LIB_OBJS}

all:	${ALLDIRS} ${EXELIST} 
dirs: ${ALLDIRS}
create_dirs_and_headers: ${ALLDIRS} 

${ALLDIRS}:	
	@test -d $@ || mkdir -p $@

-include ${DEPENDENCE_ALL}

${OBJECTS}: ${OBJ}/%.o: %.c | create_dirs_and_headers
	${CC} ${CFLAGS} ${CINCLUDE} -c -MP -MD -MF ${DEP}/$*.d -MQ ${OBJ}/$*.o -o $@ $<

${LIB_OBJS_NORMAL}: ${OBJ}/%.o: %.c | create_dirs_and_headers
	${CC} ${CFLAGS} -fpic ${CINCLUDE} -c -MP -MD -MF ${DEP}/$*.d -MQ ${OBJ}/$*.o -o $@ $<

${EXELIST}: ${LIBLIST}

${EXE}/mtproto-proxy:	${OBJ}/mtproto/mtproto-proxy.o ${OBJ}/mtproto/mtproto-config.o ${OBJ}/net/net-tcp-rpc-ext-server.o
	${CC} -o $@ $^ ${LDFLAGS}

${LIB}/libkdb.a: ${LIB_OBJS}
	rm -f $@ && ${AR_TOOL} rcs $@ $^
	${RANLIB_TOOL} $@

clean:
	rm -rf ${OBJ} ${DEP} ${EXE} || true

force-clean: clean

go-build:
	mkdir -p ${EXE}
	go build -o ${EXE}/mtproto-proxy-go ./cmd/mtproto-proxy

go-test:
	go test ./...

go-smoke: go-build
	@bash -euo pipefail -c '\
		${EXE}/mtproto-proxy-go --help > help.txt 2>&1 || test $$? -eq 2; \
		: > loop.log; \
		${EXE}/mtproto-proxy-go -l loop.log ./docker/telegram/backend.conf > /dev/null 2>&1 & \
		pid=$$!; \
		for _ in $$(seq 1 30); do \
			if grep -q "runtime initialized:" loop.log; then \
				break; \
			fi; \
			sleep 0.1; \
		done; \
		kill -USR1 $$pid; \
		sleep 0.1; \
		kill -TERM $$pid; \
		wait $$pid; \
		grep -q "SIGUSR1 received: log file reopened." loop.log; \
		grep -q "Terminated by SIGTERM." loop.log; \
		: > supervisor.log; \
		${EXE}/mtproto-proxy-go -M 2 -l supervisor.log ./docker/telegram/backend.conf > /dev/null 2>&1 & \
		spid=$$!; \
		for _ in $$(seq 1 50); do \
			if grep -q "supervisor started worker id=1" supervisor.log; then \
				break; \
			fi; \
			sleep 0.1; \
		done; \
		kill -USR1 $$spid; \
		sleep 0.1; \
		kill -TERM $$spid; \
		wait $$spid; \
		grep -q "Go bootstrap supervisor enabled: workers=2" supervisor.log; \
		grep -q "supervisor SIGUSR1: log file reopened." supervisor.log; \
		grep -q "supervisor received SIGTERM, shutting down workers" supervisor.log; \
		rm -f help.txt loop.log supervisor.log; \
	'

go-stability:
	go test ./integration/cli -run 'TestSignalLoopIngressOutboundBurstStability|TestSignalLoopOutboundIdleEvictionMetrics|TestSignalLoopOutboundMaxFrameSizeRejectsOversizedPayload|TestSignalLoopIngressOutboundSoakLoadFDAndMemoryGuards' -count=1

go-dualrun:
	MTPROXY_DUAL_RUN=1 go test ./integration/cli -run TestDualRunControlPlaneSLO -count=1

go-linux-docker-check:
	docker run --rm $(if $(DOCKER_PLATFORM),--platform $(DOCKER_PLATFORM),) -v "$$PWD":/work -w /work $(DOCKER_GO_IMAGE) bash -lc 'set -euo pipefail; export PATH=/usr/local/go/bin:$$PATH; apt-get update >/dev/null; apt-get install -y build-essential libssl-dev zlib1g-dev >/dev/null; make go-stability; make go-dualrun'
