# Plasma (plasma-nm) VPN editor plugin for Outline.
#
# plasma-nm does not install its editor headers, so we build against a source
# checkout matching the installed plasma-nm version (PLASMA_NM_SRC) and link the
# system libplasmanm_editor.so / libplasmanm_internal.so. Rebuild whenever
# plasma-nm is upgraded.
#
#   git clone --depth 1 --branch v6.6.6 https://invent.kde.org/plasma/plasma-nm.git third_party/plasma-nm
#   cmake -B build -DCMAKE_INSTALL_PREFIX=/usr && cmake --build build
cmake_minimum_required(VERSION 3.22)
project(plasma-nm-outline LANGUAGES CXX)

set(QT_MIN_VERSION 6.6)
set(KF_MIN_VERSION 6.0)

find_package(ECM ${KF_MIN_VERSION} REQUIRED NO_MODULE)
set(CMAKE_MODULE_PATH ${ECM_MODULE_PATH})
include(KDEInstallDirs)
include(KDECMakeSettings)
include(KDECompilerSettings NO_POLICY_SCOPE)

find_package(Qt6 ${QT_MIN_VERSION} REQUIRED COMPONENTS Widgets DBus)
find_package(KF6 ${KF_MIN_VERSION} REQUIRED COMPONENTS CoreAddons I18n WidgetsAddons NetworkManagerQt)
find_package(PkgConfig REQUIRED)
pkg_check_modules(NM REQUIRED IMPORTED_TARGET libnm)

set(PLASMA_NM_SRC ${CMAKE_CURRENT_SOURCE_DIR}/third_party/plasma-nm CACHE PATH "plasma-nm source tree (headers only)")
if(NOT EXISTS ${PLASMA_NM_SRC}/libs/editor/vpnuiplugin.h)
    message(FATAL_ERROR "plasma-nm sources not found at ${PLASMA_NM_SRC}; see header of this file")
endif()

find_library(PLASMANM_EDITOR plasmanm_editor REQUIRED)
find_library(PLASMANM_INTERNAL plasmanm_internal REQUIRED)

add_definitions(-DTRANSLATION_DOMAIN=\"plasmanetworkmanagement_outlineui\")

add_library(plasmanetworkmanagement_outlineui MODULE
    src/outline.cpp
    src/outlinewidget.cpp
    src/outlineauth.cpp
)
ki18n_wrap_ui(plasmanetworkmanagement_outlineui src/outlinewidget.ui src/outlineauth.ui)

target_include_directories(plasmanetworkmanagement_outlineui PRIVATE
    ${PLASMA_NM_SRC}/libs
    ${PLASMA_NM_SRC}/libs/editor
    ${PLASMA_NM_SRC}/libs/editor/widgets
    ${CMAKE_CURRENT_BINARY_DIR}
)
target_link_libraries(plasmanetworkmanagement_outlineui
    ${PLASMANM_EDITOR}
    ${PLASMANM_INTERNAL}
    Qt6::Widgets
    KF6::CoreAddons
    KF6::I18n
    KF6::WidgetsAddons
    KF6::NetworkManagerQt
    PkgConfig::NM
)

install(TARGETS plasmanetworkmanagement_outlineui DESTINATION ${KDE_INSTALL_PLUGINDIR}/plasma/network/vpn)
