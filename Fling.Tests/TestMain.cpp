#include "pch.h"

// Own main instead of gtest_main: vcpkg puts gtest_main.lib in manual-link, which auto-link skips.
int main(int argc, char** argv)
{
    ::testing::InitGoogleTest(&argc, argv);
    return RUN_ALL_TESTS();
}
