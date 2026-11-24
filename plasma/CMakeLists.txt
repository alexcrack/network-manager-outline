# Plasma (plasma-nm) VPN editor plugin for Outline.
#
# plasma-nm does not install its editor headers, so we build against a source
# checkout matching the installed plasma-nm version (PLASMA_NM_SRC, headers
# only) and link the installed libplasmanm_editor.so. Rebuild whenever
# plasma-nm is upgraded. `make plasma` in the top directory does all of this.
cmake_minimum_required(VERSION 3.22)
project(plasma-nm-outline LANGUAGES CXX)

set(QT_MIN_VERSION 6.6)
set(KF_MIN_VERSION 6.0)

find_package(ECM ${KF_MIN_VERSION} REQUIRED NO_MODULE)
set(CMAKE_MODULE_PATH ${ECM_MODULE_PATH})
include(KDEInstallDirs)
include(KDECMakeSettings)
include(KDECompilerSettings NO_POLICY_SCOPE)
include(ECMAddTests)

find_package(Qt6 ${QT_MIN_VERSION} REQUIRED COMPONENTS Widgets Network)
find_package(KF6 ${KF_MIN_VERSION} REQUIRED COMPONENTS CoreAddons I18n WidgetsAddons NetworkManagerQt)
find_package(PkgConfig REQUIRED)
pkg_check_modules(NM REQUIRED IMPORTED_TARGET libnm)

set(PLASMA_NM_SRC ${CMAKE_CURRENT_SOURCE_DIR}/third_party/plasma-nm CACHE PATH "plasma-nm source tree (headers only)")
if(NOT EXISTS ${PLASMA_NM_SRC}/libs/editor/vpnuiplugin.h)
    message(FATAL_ERROR "plasma-nm sources not found at ${PLASMA_NM_SRC}; run `make plasma` in the top directory")
endif()

find_library(PLASMANM_EDITOR NAMES plasmanm_editor libplasmanm_editor.so REQUIRED)
message(STATUS "Linking ${PLASMANM_EDITOR}")

add_definitions(-DTRANSLATION_DOMAIN=\"plasmanetworkmanagement_outlineui\")
# KDE's default flags forbid implicit QString(const char*) conversions; plasma-nm headers are fine with that.

# Everything except the plugin entry point, shared with the test.
add_library(outlineui_static STATIC
    src/outlinewidget.cpp
    src/outlineauth.cpp
    src/outlineutils.cpp
)
ki18n_wrap_ui(outlineui_static src/outlinewidget.ui src/outlineauth.ui)
set_target_properties(outlineui_static PROPERTIES POSITION_INDEPENDENT_CODE ON)
target_include_directories(outlineui_static PUBLIC
    ${CMAKE_CURRENT_SOURCE_DIR}/src
    ${CMAKE_CURRENT_SOURCE_DIR}/src/compat   # stand-in for plasma-nm's generated export header
    ${PLASMA_NM_SRC}/libs/editor
    ${PLASMA_NM_SRC}/libs/editor/widgets
    ${CMAKE_CURRENT_BINARY_DIR}
)
target_link_libraries(outlineui_static PUBLIC
    ${PLASMANM_EDITOR}
    Qt6::Widgets
    Qt6::Network
    KF6::CoreAddons
    KF6::I18n
    KF6::WidgetsAddons
    KF6::NetworkManagerQt
    PkgConfig::NM
)

add_library(plasmanetworkmanagement_outlineui MODULE src/outline.cpp)
target_link_libraries(plasmanetworkmanagement_outlineui PRIVATE outlineui_static)

install(TARGETS plasmanetworkmanagement_outlineui DESTINATION ${KDE_INSTALL_PLUGINDIR}/plasma/network/vpn)

if(BUILD_TESTING)
    find_package(Qt6 ${QT_MIN_VERSION} REQUIRED COMPONENTS Test)
    add_subdirectory(tests)
endif()
