// Adapter to compile the XAML-compiler-generated XamlMetaDataProvider.g.cpp.
// The Windows App SDK XAML compiler emits Generated Files\XamlMetaDataProvider.cpp
// alongside .g.cpp, but MSBuild in this project silently deduplicates it out of the
// ClCompile list (likely collides with a CppWinRT auto-add). This file under the
// project root sits at a unique path that MSBuild won't collapse.

#include "pch.h"
#include "XamlMetaDataProvider.h"
#include "XamlMetaDataProvider.g.cpp"
