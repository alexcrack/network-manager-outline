// Stand-in for the header plasma-nm generates at build time with
// generate_export_header(plasmanm_editor). It is not in plasma-nm's source
// tree, and we only import symbols from the installed libplasmanm_editor.so.
#pragma once

#include <QtGlobal>

#define PLASMANM_EDITOR_EXPORT Q_DECL_IMPORT
#define PLASMANM_EDITOR_NO_EXPORT
