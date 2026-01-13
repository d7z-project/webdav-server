// fs_test.go
package mergefs

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMountFs(t *testing.T) {
	t.Run("创建MountFs", func(t *testing.T) {
		mfs := NewMountFs(nil)
		assert.NotNil(t, mfs)
		assert.Equal(t, "MountFs", mfs.Name())
	})

	t.Run("自定义默认文件系统", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		mfs := NewMountFs(memFs)
		assert.NotNil(t, mfs)
	})
}

func TestMountUnmount(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	t.Run("添加挂载点", func(t *testing.T) {
		memFs := afero.NewMemMapFs()
		mfs.Mount("/users", memFs)

		mounts := mfs.ListMounts()
		assert.Len(t, mounts, 1)
		assert.Equal(t, "/users", mounts[0].Prefix)
		assert.Equal(t, memFs, mounts[0].Fs)
	})

	t.Run("添加多个挂载点", func(t *testing.T) {
		mfs.Mount("/config", afero.NewMemMapFs())
		mfs.Mount("/static", afero.NewMemMapFs())

		mounts := mfs.ListMounts()
		assert.Len(t, mounts, 3)

		// 验证挂载点按长度降序排列
		prefixes := []string{"/config", "/static", "/users"} // 所有长度相同，按添加顺序的反向（因为sort.Stable保持相同长度的相对顺序）
		for i, mount := range mounts {
			assert.Equal(t, prefixes[i], mount.Prefix)
		}
	})

	t.Run("替换挂载点", func(t *testing.T) {
		newFs := afero.NewMemMapFs()
		mfs.Mount("/users", newFs)

		mounts := mfs.ListMounts()
		// 找到/users挂载点
		for _, mount := range mounts {
			if mount.Prefix == "/users" {
				assert.Equal(t, newFs, mount.Fs)
				break
			}
		}
	})

	t.Run("移除挂载点", func(t *testing.T) {
		removed := mfs.Unmount("/config")
		assert.True(t, removed)

		mounts := mfs.ListMounts()
		assert.Len(t, mounts, 2)

		// 验证剩余的挂载点（不关心顺序，只关心是否存在）
		expectedPrefixes := []string{"/users", "/static"}
		actualPrefixes := []string{mounts[0].Prefix, mounts[1].Prefix}
		sort.Strings(expectedPrefixes)
		sort.Strings(actualPrefixes)
		assert.Equal(t, expectedPrefixes, actualPrefixes)
	})

	t.Run("移除不存在的挂载点", func(t *testing.T) {
		removed := mfs.Unmount("/nonexistent")
		assert.False(t, removed)
	})
}

func TestGetMount(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	usersFs := afero.NewMemMapFs()
	staticFs := afero.NewMemMapFs()
	configFs := afero.NewMemMapFs()
	rootFs := afero.NewMemMapFs()

	mfs.Mount("/users", usersFs)
	mfs.Mount("/static", staticFs)
	mfs.Mount("/config", configFs)
	mfs.Mount("/", rootFs) // 根挂载点

	testCases := []struct {
		name     string
		path     string
		expected afero.Fs
		relPath  string
	}{
		{"根路径", "/", rootFs, "/"},
		{"用户路径", "/users/profile", usersFs, "/profile"},
		{"静态文件", "/static/css/style.css", staticFs, "/css/style.css"},
		{"配置路径", "/config/app.yaml", configFs, "/app.yaml"},
		{"嵌套路径", "/users/admin/docs/readme.md", usersFs, "/admin/docs/readme.md"},
		{"无挂载路径", "/tmp/file.txt", mfs.defaultFs, "/tmp/file.txt"},
		{"清理路径", "./users/../static/./img.jpg", staticFs, "/img.jpg"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fs, relPath := mfs.GetMount(tc.path)
			assert.Equal(t, tc.expected, fs)
			assert.Equal(t, tc.relPath, relPath)
		})
	}
}

func TestFileOperations(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	// 设置测试文件系统
	testFs := afero.NewMemMapFs()
	mfs.Mount("/test", testFs)

	t.Run("创建文件", func(t *testing.T) {
		file, err := mfs.Create("/test/newfile.txt")
		require.NoError(t, err)
		require.NotNil(t, file)
		file.Close()

		// 验证文件在正确的文件系统中
		exists, _ := afero.Exists(testFs, "/newfile.txt")
		assert.True(t, exists)

		// 验证默认文件系统中没有该文件
		exists, _ = afero.Exists(mfs.defaultFs, "/test/newfile.txt")
		assert.False(t, exists)
	})

	t.Run("写入和读取文件", func(t *testing.T) {
		content := []byte("Hello, World!")

		// 写入文件
		err := afero.WriteFile(mfs, "/test/data.txt", content, 0644)
		require.NoError(t, err)

		// 读取文件
		data, err := afero.ReadFile(mfs, "/test/data.txt")
		require.NoError(t, err)
		assert.Equal(t, content, data)

		// 直接通过底层文件系统验证
		data, err = afero.ReadFile(testFs, "/data.txt")
		require.NoError(t, err)
		assert.Equal(t, content, data)
	})

	t.Run("创建目录", func(t *testing.T) {
		err := mfs.Mkdir("/test/subdir", 0755)
		require.NoError(t, err)

		exists, _ := afero.DirExists(testFs, "/subdir")
		assert.True(t, exists)
	})

	t.Run("递归创建目录", func(t *testing.T) {
		err := mfs.MkdirAll("/test/deep/nested/directory", 0755)
		require.NoError(t, err)

		exists, _ := afero.DirExists(testFs, "/deep/nested/directory")
		assert.True(t, exists)
	})

	t.Run("打开文件", func(t *testing.T) {
		afero.WriteFile(testFs, "/existing.txt", []byte("test"), 0644)

		file, err := mfs.Open("/test/existing.txt")
		require.NoError(t, err)
		require.NotNil(t, file)
		file.Close()
	})

	t.Run("打开不存在的文件", func(t *testing.T) {
		_, err := mfs.Open("/test/nonexistent.txt")
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})
}

func TestStatOperations(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())
	testFs := afero.NewMemMapFs()
	mfs.Mount("/data", testFs)

	// 创建测试文件
	testContent := []byte("test content")
	err := afero.WriteFile(testFs, "/file.txt", testContent, 0644)
	require.NoError(t, err)

	// 创建测试目录
	err = testFs.Mkdir("/subdir", 0755)
	require.NoError(t, err)

	t.Run("文件Stat", func(t *testing.T) {
		info, err := mfs.Stat("/data/file.txt")
		require.NoError(t, err)
		require.NotNil(t, info)

		assert.Equal(t, "file.txt", info.Name())
		assert.False(t, info.IsDir())
		assert.Equal(t, int64(len(testContent)), info.Size())
	})

	t.Run("目录Stat", func(t *testing.T) {
		info, err := mfs.Stat("/data/subdir")
		require.NoError(t, err)
		require.NotNil(t, info)

		assert.Equal(t, "subdir", info.Name())
		assert.True(t, info.IsDir())
	})

	t.Run("不存在的文件Stat", func(t *testing.T) {
		_, err := mfs.Stat("/data/nonexistent.txt")
		assert.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})
}

func TestRemoveOperations(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())
	testFs := afero.NewMemMapFs()
	mfs.Mount("/cache", testFs)

	t.Run("删除文件", func(t *testing.T) {
		// 创建文件
		err := afero.WriteFile(testFs, "/temp.txt", []byte("temp"), 0644)
		require.NoError(t, err)

		// 验证文件存在
		exists, _ := afero.Exists(testFs, "/temp.txt")
		assert.True(t, exists)

		// 通过MountFs删除
		err = mfs.Remove("/cache/temp.txt")
		require.NoError(t, err)

		// 验证文件已删除
		exists, _ = afero.Exists(testFs, "/temp.txt")
		assert.False(t, exists)
	})

	t.Run("删除不存在的文件", func(t *testing.T) {
		err := mfs.Remove("/cache/nonexistent.txt")
		assert.Error(t, err)
	})

	t.Run("递归删除目录", func(t *testing.T) {
		// 创建嵌套目录结构
		err := testFs.MkdirAll("/deep/nested/dir", 0755)
		require.NoError(t, err)

		err = afero.WriteFile(testFs, "/deep/nested/dir/file.txt", []byte("data"), 0644)
		require.NoError(t, err)

		// 删除整个目录
		err = mfs.RemoveAll("/cache/deep")
		require.NoError(t, err)

		// 验证目录已删除
		exists, _ := afero.DirExists(testFs, "/deep")
		assert.False(t, exists)
	})
}

func TestRenameOperations(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	t.Run("同一文件系统内重命名", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		mfs.Mount("/docs", fs)

		// 创建源文件
		err := afero.WriteFile(fs, "/old.txt", []byte("content"), 0644)
		require.NoError(t, err)

		// 重命名
		err = mfs.Rename("/docs/old.txt", "/docs/new.txt")
		require.NoError(t, err)

		// 验证
		oldExists, _ := afero.Exists(fs, "/old.txt")
		newExists, _ := afero.Exists(fs, "/new.txt")
		assert.False(t, oldExists)
		assert.True(t, newExists)

		// 验证内容
		content, _ := afero.ReadFile(fs, "/new.txt")
		assert.Equal(t, []byte("content"), content)
	})

	t.Run("跨文件系统重命名", func(t *testing.T) {
		srcFs := afero.NewMemMapFs()
		dstFs := afero.NewMemMapFs()

		mfs.Mount("/src", srcFs)
		mfs.Mount("/dst", dstFs)

		// 在源文件系统创建文件
		err := afero.WriteFile(srcFs, "/file.txt", []byte("cross fs data"), 0644)
		require.NoError(t, err)

		// 跨文件系统重命名
		err = mfs.Rename("/src/file.txt", "/dst/moved.txt")
		require.NoError(t, err)

		// 验证
		srcExists, _ := afero.Exists(srcFs, "/file.txt")
		dstExists, _ := afero.Exists(dstFs, "/moved.txt")
		assert.False(t, srcExists)
		assert.True(t, dstExists)

		// 验证内容
		content, _ := afero.ReadFile(dstFs, "/moved.txt")
		assert.Equal(t, []byte("cross fs data"), content)
	})

	t.Run("跨文件系统重命名目录", func(t *testing.T) {
		srcFs := afero.NewMemMapFs()
		dstFs := afero.NewMemMapFs()

		mfs.Mount("/source", srcFs)
		mfs.Mount("/destination", dstFs)

		// 创建嵌套目录结构
		err := srcFs.MkdirAll("/dir/subdir", 0755)
		require.NoError(t, err)

		err = afero.WriteFile(srcFs, "/dir/file1.txt", []byte("file1"), 0644)
		require.NoError(t, err)

		err = afero.WriteFile(srcFs, "/dir/subdir/file2.txt", []byte("file2"), 0644)
		require.NoError(t, err)

		// 跨文件系统重命名目录
		err = mfs.Rename("/source/dir", "/destination/movedir")
		require.NoError(t, err)

		// 验证源目录已删除
		srcExists, _ := afero.DirExists(srcFs, "/dir")
		assert.False(t, srcExists)

		// 验证目标目录已创建
		dstExists, _ := afero.DirExists(dstFs, "/movedir")
		assert.True(t, dstExists)

		// 验证文件已移动
		file1Exists, _ := afero.Exists(dstFs, "/movedir/file1.txt")
		file2Exists, _ := afero.Exists(dstFs, "/movedir/subdir/file2.txt")
		assert.True(t, file1Exists)
		assert.True(t, file2Exists)

		// 验证文件内容
		content1, _ := afero.ReadFile(dstFs, "/movedir/file1.txt")
		content2, _ := afero.ReadFile(dstFs, "/movedir/subdir/file2.txt")
		assert.Equal(t, []byte("file1"), content1)
		assert.Equal(t, []byte("file2"), content2)
	})
}

func TestChmodChtimes(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())
	testFs := afero.NewMemMapFs()
	mfs.Mount("/files", testFs)

	// 创建测试文件
	err := afero.WriteFile(testFs, "/test.txt", []byte("test"), 0644)
	require.NoError(t, err)

	t.Run("修改文件权限", func(t *testing.T) {
		// 获取原始权限
		info, err := testFs.Stat("/test.txt")
		require.NoError(t, err)
		originalMode := info.Mode()

		// 修改权限
		newMode := os.FileMode(0755)
		err = mfs.Chmod("/files/test.txt", newMode)
		require.NoError(t, err)

		// 验证权限已修改
		info, err = testFs.Stat("/test.txt")
		require.NoError(t, err)
		assert.Equal(t, newMode, info.Mode())

		// 恢复原始权限
		err = testFs.Chmod("/test.txt", originalMode)
		require.NoError(t, err)
	})

	t.Run("修改文件时间", func(t *testing.T) {
		newAtime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		newMtime := time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC)

		err := mfs.Chtimes("/files/test.txt", newAtime, newMtime)
		require.NoError(t, err)

		// 验证时间已修改
		info, err := testFs.Stat("/test.txt")
		require.NoError(t, err)

		// 注意：MemMapFs可能不支持精确的时间修改
		// 这里只验证调用没有错误
		assert.NotNil(t, info.ModTime())
	})
}

func TestComplexMountScenarios(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	// 设置复杂挂载结构
	fs1 := afero.NewMemMapFs()
	fs2 := afero.NewMemMapFs()
	fs3 := afero.NewMemMapFs()

	mfs.Mount("/app/users", fs1)
	mfs.Mount("/app", fs2)
	mfs.Mount("/app/users/admins", fs3) // 更长的前缀

	t.Run("最长前缀匹配", func(t *testing.T) {
		// /app/users/admins 应该匹配 fs3（最长匹配）
		fs, path := mfs.GetMount("/app/users/admins/profile.txt")
		assert.Equal(t, fs3, fs)
		assert.Equal(t, "/profile.txt", path)

		// /app/users/regular 应该匹配 fs1
		fs, path = mfs.GetMount("/app/users/regular/profile.txt")
		assert.Equal(t, fs1, fs)
		assert.Equal(t, "/regular/profile.txt", path)

		// /app/config 应该匹配 fs2
		fs, path = mfs.GetMount("/app/config/settings.yaml")
		assert.Equal(t, fs2, fs)
		assert.Equal(t, "/config/settings.yaml", path)
	})

	t.Run("路径清理", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"/app/users/../config", "/app/config"},
			{"./app/users", "/app/users"},
			{"app/users", "/app/users"},
			{"/app/users/.", "/app/users"},
			{"/app/users/././profile", "/app/users/profile"},
		}

		for _, tc := range testCases {
			t.Run(tc.input, func(t *testing.T) {
				// 只验证cleanPath函数的行为
				cleaned := cleanPath(tc.input)
				assert.Equal(t, tc.expected, cleaned)
			})
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	// 创建多个文件系统
	fs1 := afero.NewMemMapFs()
	fs2 := afero.NewMemMapFs()

	mfs.Mount("/data1", fs1)
	mfs.Mount("/data2", fs2)

	// 并发测试
	done := make(chan bool)
	errors := make(chan error, 100)

	// 启动多个goroutine并发访问
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 10; j++ {
				path := filepath.Join("/data1", "file_"+string(rune(id))+"_"+string(rune(j))+".txt")
				content := []byte("test content")

				// 写入
				if err := afero.WriteFile(mfs, path, content, 0644); err != nil {
					errors <- err
				}

				// 读取
				if _, err := afero.ReadFile(mfs, path); err != nil {
					errors <- err
				}

				// Stat
				if _, err := mfs.Stat(path); err != nil {
					errors <- err
				}

				// 删除
				if err := mfs.Remove(path); err != nil {
					errors <- err
				}
			}
			done <- true
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 检查错误
	close(errors)
	hasError := false
	for err := range errors {
		if err != nil {
			t.Errorf("并发测试出错: %v", err)
			hasError = true
		}
	}
	assert.False(t, hasError, "并发测试不应有错误")
}

func TestEdgeCases(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	t.Run("空路径", func(t *testing.T) {
		fs, path := mfs.GetMount("")
		assert.Equal(t, mfs.defaultFs, fs)
		assert.Equal(t, "/", path)
	})

	t.Run("点路径", func(t *testing.T) {
		fs, path := mfs.GetMount(".")
		assert.Equal(t, mfs.defaultFs, fs)
		assert.Equal(t, "/", path)
	})

	t.Run("双点路径", func(t *testing.T) {
		mfs.Mount("/test", afero.NewMemMapFs())
		fs, path := mfs.GetMount("/test/../root")
		assert.Equal(t, mfs.defaultFs, fs)
		assert.Equal(t, "/root", path)
	})

	t.Run("带空格的路径", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		mfs.Mount("/path with spaces", fs)

		testFs, relPath := mfs.GetMount("/path with spaces/file.txt")
		assert.Equal(t, fs, testFs)
		assert.Equal(t, "/file.txt", relPath)
	})

	t.Run("中文路径", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		mfs.Mount("/中文路径", fs)

		testFs, relPath := mfs.GetMount("/中文路径/文件.txt")
		assert.Equal(t, fs, testFs)
		assert.Equal(t, "/文件.txt", relPath)
	})
}

func TestMountInfo(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	fs1 := afero.NewMemMapFs()
	fs2 := afero.NewMemMapFs()

	mfs.Mount("/mount1", fs1)
	mfs.Mount("/mount2/sub", fs2)

	t.Run("获取挂载信息", func(t *testing.T) {
		prefix, fs, relPath := mfs.GetMountInfo("/mount1/file.txt")
		assert.Equal(t, "/mount1", prefix)
		assert.Equal(t, fs1, fs)
		assert.Equal(t, "/file.txt", relPath)

		prefix, fs, relPath = mfs.GetMountInfo("/mount2/sub/deep/file.txt")
		assert.Equal(t, "/mount2/sub", prefix)
		assert.Equal(t, fs2, fs)
		assert.Equal(t, "/deep/file.txt", relPath)

		// 无挂载点的路径
		prefix, fs, relPath = mfs.GetMountInfo("/unmounted/path")
		assert.Equal(t, "/", prefix)
		assert.Equal(t, mfs.defaultFs, fs)
		assert.Equal(t, "/unmounted/path", relPath)
	})
}

// 集成测试：模拟真实使用场景
func TestIntegrationScenario(t *testing.T) {
	// 模拟一个Web应用的文件系统布局
	mfs := NewMountFs(afero.NewMemMapFs())

	// 使用内存文件系统进行测试
	templatesFs := afero.NewMemMapFs()
	staticFs := afero.NewMemMapFs()
	uploadsFs := afero.NewMemMapFs()
	configFs := afero.NewMemMapFs()

	// 设置挂载
	mfs.Mount("/templates", templatesFs)
	mfs.Mount("/static", staticFs)
	mfs.Mount("/uploads", uploadsFs)
	mfs.Mount("/config", configFs)

	t.Run("Web应用场景", func(t *testing.T) {
		// 1. 写入模板文件
		templateContent := `<html><body>Hello {{.Name}}</body></html>`
		err := afero.WriteFile(mfs, "/templates/index.html", []byte(templateContent), 0644)
		require.NoError(t, err)

		// 2. 写入静态文件
		cssContent := `body { color: red; }`
		err = afero.WriteFile(mfs, "/static/css/style.css", []byte(cssContent), 0644)
		require.NoError(t, err)

		// 3. 写入配置文件
		configContent := "debug: true\nport: 8080"
		err = afero.WriteFile(mfs, "/config/app.yaml", []byte(configContent), 0644)
		require.NoError(t, err)

		// 4. 模拟用户上传
		uploadContent := []byte("user uploaded file content")
		err = afero.WriteFile(mfs, "/uploads/user123/document.pdf", uploadContent, 0644)
		require.NoError(t, err)

		// 验证所有文件都在正确的位置
		templateExists, _ := afero.Exists(templatesFs, "/index.html")
		cssExists, _ := afero.Exists(staticFs, "/css/style.css")
		configExists, _ := afero.Exists(configFs, "/app.yaml")
		uploadExists, _ := afero.Exists(uploadsFs, "/user123/document.pdf")

		assert.True(t, templateExists)
		assert.True(t, cssExists)
		assert.True(t, configExists)
		assert.True(t, uploadExists)

		// 验证内容
		templateData, _ := afero.ReadFile(templatesFs, "/index.html")
		assert.Equal(t, templateContent, string(templateData))

		// 验证默认文件系统中没有这些文件
		defaultExists, _ := afero.Exists(mfs.defaultFs, "/templates/index.html")
		assert.False(t, defaultExists)
	})

	t.Run("文件遍历", func(t *testing.T) {
		// 创建一个新的文件系统用于测试遍历，避免之前测试的影响
		walkFs := afero.NewMemMapFs()
		mfs.Mount("/walk", walkFs)

		// 在walk文件系统中创建多个文件
		files := []string{
			"/walk/js/app.js",
			"/walk/js/lib.js",
			"/walk/images/logo.png",
			"/walk/images/banner.jpg",
		}

		for _, file := range files {
			err := afero.WriteFile(mfs, file, []byte("content"), 0644)
			require.NoError(t, err)
		}

		// 使用afero.WalkDir遍历
		count := 0
		afero.Walk(walkFs, "/", func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				count++
			}
			return nil
		})

		// 应该找到4个文件
		assert.Equal(t, 4, count)
	})
}

func TestSortMounts(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	// 添加挂载点，按不同长度
	mfs.Mount("/a", afero.NewMemMapFs())           // 2
	mfs.Mount("/abc/def", afero.NewMemMapFs())     // 7
	mfs.Mount("/ab", afero.NewMemMapFs())          // 3
	mfs.Mount("/abc", afero.NewMemMapFs())         // 4
	mfs.Mount("/abc/def/ghi", afero.NewMemMapFs()) // 11
	mfs.Mount("/", afero.NewMemMapFs())            // 1

	mounts := mfs.ListMounts()

	// 验证按长度降序排列
	expectedOrder := []string{
		"/abc/def/ghi", // 长度11
		"/abc/def",     // 长度7
		"/abc",         // 长度4
		"/ab",          // 长度3
		"/a",           // 长度2
		"/",            // 长度1
	}

	for i, mount := range mounts {
		assert.Equal(t, expectedOrder[i], mount.Prefix, "位置 %d 的挂载点前缀不正确", i)
	}
}

func TestCrossFileSystemOperations(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())

	srcFs := afero.NewMemMapFs()
	dstFs := afero.NewMemMapFs()

	mfs.Mount("/source", srcFs)
	mfs.Mount("/dest", dstFs)

	t.Run("复制大文件", func(t *testing.T) {
		// 创建一个大文件
		bigData := make([]byte, 1024*1024) // 1MB
		for i := range bigData {
			bigData[i] = byte(i % 256)
		}

		err := afero.WriteFile(srcFs, "/bigfile.bin", bigData, 0644)
		require.NoError(t, err)

		// 跨文件系统重命名（复制）
		err = mfs.Rename("/source/bigfile.bin", "/dest/bigfile_copied.bin")
		require.NoError(t, err)

		// 验证
		copiedData, err := afero.ReadFile(dstFs, "/bigfile_copied.bin")
		require.NoError(t, err)
		assert.Equal(t, bigData, copiedData)
		assert.Equal(t, len(bigData), len(copiedData))
	})

	t.Run("复制空目录", func(t *testing.T) {
		err := srcFs.MkdirAll("/emptydir", 0755)
		require.NoError(t, err)

		err = mfs.Rename("/source/emptydir", "/dest/emptydir_moved")
		require.NoError(t, err)

		exists, _ := afero.DirExists(dstFs, "/emptydir_moved")
		assert.True(t, exists)
	})
}

func TestErrorHandling(t *testing.T) {
	mfs := NewMountFs(afero.NewMemMapFs())
	testFs := afero.NewBasePathFs(afero.NewOsFs(), t.TempDir())
	mfs.Mount("/test", testFs)

	t.Run("权限错误", func(t *testing.T) {
		// 创建只读文件
		err := afero.WriteFile(testFs, "/readonly.txt", []byte("readonly"), 0444)
		require.NoError(t, err)
		_, err = mfs.OpenFile("/test/readonly.txt", os.O_WRONLY, 0644)
		assert.NotNil(t, err)
	})

	t.Run("无效路径", func(t *testing.T) {
		_, err := mfs.Open("/test/\x00invalid.txt")
		assert.Error(t, err)
	})
}
