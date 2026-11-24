ecm_add_test(plugintest.cpp
    TEST_NAME plugintest
    LINK_LIBRARIES outlineui_static Qt6::Test
)
target_compile_definitions(plugintest PRIVATE
    PLUGIN_PATH="$<TARGET_FILE:plasmanetworkmanagement_outlineui>")
add_dependencies(plugintest plasmanetworkmanagement_outlineui)
set_tests_properties(plugintest PROPERTIES ENVIRONMENT "QT_QPA_PLATFORM=offscreen")
