/*
    This file is part of Mtproto-proxy Library.

    Mtproto-proxy Library is free software: you can redistribute it and/or modify
    it under the terms of the GNU Lesser General Public License as published by
    the Free Software Foundation, either version 2 of the License, or
    (at your option) any later version.

    Mtproto-proxy Library is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Lesser General Public License for more details.
*/

#pragma once

#ifndef EPOLL_SHIM_DISABLE_WRAPPER_MACROS
#define EPOLL_SHIM_DISABLE_WRAPPER_MACROS
#endif

#if defined(__has_include)
#if __has_include(<sys/epoll.h>)
#include <sys/epoll.h>
#elif __has_include(<libepoll-shim/sys/epoll.h>)
#include <libepoll-shim/sys/epoll.h>
#else
#error "epoll headers were not found. Install epoll-shim on macOS."
#endif
#else
#include <sys/epoll.h>
#endif
