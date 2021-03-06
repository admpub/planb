// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package router

import (
	"bytes"
	"testing"
	"time"

	"github.com/tsuru/planb/log"
	"github.com/tsuru/planb/reverseproxy"
	"gopkg.in/check.v1"
	"gopkg.in/redis.v3"
)

type S struct {
	redis *redis.Client
}

var _ = check.Suite(&S{})

func Test(t *testing.T) {
	check.TestingT(t)
}

func clearKeys(r *redis.Client) error {
	val := r.Keys("frontend:*").Val()
	val = append(val, r.Keys("dead:*").Val()...)
	if len(val) > 0 {
		return r.Del(val...).Err()
	}
	return nil
}

func redisConn() (*redis.Client, error) {
	return redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 0}), nil
}

func (s *S) SetUpTest(c *check.C) {
	var err error
	s.redis, err = redisConn()
	c.Assert(err, check.IsNil)
	err = clearKeys(s.redis)
	c.Assert(err, check.IsNil)
}

func (s *S) TearDownTest(c *check.C) {
	s.redis.Close()
}

func (s *S) TestInit(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	c.Assert(router.roundRobin, check.DeepEquals, map[string]*int32{})
	c.Assert(router.logger, check.IsNil)
	c.Assert(router.cache, check.NotNil)
	c.Assert(router.Backend, check.NotNil)
}

func (s *S) TestChooseBackend(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	err = s.redis.RPush("frontend:myfrontend.com", "myfrontend", "http://url1:123").Err()
	c.Assert(err, check.IsNil)
	reqData, err := router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.IsNil)
	c.Assert(reqData.StartTime.IsZero(), check.Equals, false)
	reqData.StartTime = time.Time{}
	c.Assert(reqData, check.DeepEquals, &reverseproxy.RequestData{
		Backend:    "http://url1:123",
		BackendIdx: 0,
		BackendKey: "myfrontend.com",
		BackendLen: 1,
		Host:       "myfrontend.com",
	})
}

func (s *S) TestChooseBackendNotFound(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	reqData, err := router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.ErrorMatches, `error running routes backend commands: no backends available`)
	c.Assert(reqData.StartTime.IsZero(), check.Equals, false)
	reqData.StartTime = time.Time{}
	c.Assert(reqData, check.DeepEquals, &reverseproxy.RequestData{
		Backend:    "",
		BackendIdx: 0,
		BackendLen: 0,
		BackendKey: "",
		Host:       "myfrontend.com",
	})
}

func (s *S) TestChooseBackendNoBackends(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	err = s.redis.RPush("frontend:myfrontend.com", "myfrontend").Err()
	c.Assert(err, check.IsNil)
	reqData, err := router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.ErrorMatches, `error running routes backend commands: no backends available`)
	c.Assert(reqData.StartTime.IsZero(), check.Equals, false)
	reqData.StartTime = time.Time{}
	c.Assert(reqData, check.DeepEquals, &reverseproxy.RequestData{
		Backend:    "",
		BackendIdx: 0,
		BackendLen: 0,
		BackendKey: "",
		Host:       "myfrontend.com",
	})
}

func (s *S) TestChooseBackendAllDead(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	err = s.redis.RPush("frontend:myfrontend.com", "myfrontend", "http://url1:123").Err()
	c.Assert(err, check.IsNil)
	err = s.redis.SAdd("dead:myfrontend.com", "0").Err()
	c.Assert(err, check.IsNil)
	reqData, err := router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.ErrorMatches, `all backends are dead`)
	c.Assert(reqData.StartTime.IsZero(), check.Equals, false)
	reqData.StartTime = time.Time{}
	c.Assert(reqData, check.DeepEquals, &reverseproxy.RequestData{
		Backend:    "",
		BackendIdx: 0,
		BackendLen: 1,
		BackendKey: "myfrontend.com",
		Host:       "myfrontend.com",
	})
}

func (s *S) TestChooseBackendRoundRobin(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	err = s.redis.RPush("frontend:myfrontend.com", "myfrontend", "http://url1:123", "http://url2:123", "http://url3:123").Err()
	c.Assert(err, check.IsNil)
	reqData, err := router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.IsNil)
	c.Assert(reqData.StartTime.IsZero(), check.Equals, false)
	reqData.StartTime = time.Time{}
	c.Assert(reqData, check.DeepEquals, &reverseproxy.RequestData{
		Backend:    "http://url1:123",
		BackendIdx: 0,
		BackendKey: "myfrontend.com",
		BackendLen: 3,
		Host:       "myfrontend.com",
	})
	reqData, err = router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.IsNil)
	c.Assert(reqData.Backend, check.Equals, "http://url2:123")
	reqData, err = router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.IsNil)
	c.Assert(reqData.Backend, check.Equals, "http://url3:123")
	reqData, err = router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.IsNil)
	c.Assert(reqData.Backend, check.Equals, "http://url1:123")
}

func (s *S) TestChooseBackendRoundRobinNoCache(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	err = s.redis.RPush("frontend:myfrontend.com", "myfrontend", "http://url1:123", "http://url2:123", "http://url3:123").Err()
	c.Assert(err, check.IsNil)
	router.cache.Purge()
	reqData, err := router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.IsNil)
	c.Assert(reqData.Backend, check.Equals, "http://url1:123")
	router.cache.Purge()
	reqData, err = router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.IsNil)
	c.Assert(reqData.Backend, check.Equals, "http://url2:123")
	router.cache.Purge()
	reqData, err = router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.IsNil)
	c.Assert(reqData.Backend, check.Equals, "http://url3:123")
	router.cache.Purge()
	reqData, err = router.ChooseBackend("myfrontend.com")
	c.Assert(err, check.IsNil)
	c.Assert(reqData.Backend, check.Equals, "http://url1:123")
}

type bufferCloser struct {
	bytes.Buffer
}

func (b *bufferCloser) Close() error {
	return nil
}

func (s *S) TestEndRequest(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	buf := bufferCloser{}
	router.logger = log.NewWriterLogger(&buf)
	data := &reverseproxy.RequestData{
		Host: "myfe.com",
	}
	err = router.EndRequest(data, false, nil)
	c.Assert(err, check.IsNil)
	members := s.redis.SMembers("dead:myfe.com").Val()
	c.Assert(members, check.DeepEquals, []string{})
	router.Stop()
	c.Assert(buf.String(), check.Equals, "")
}

func (s *S) TestEndRequestWithLogFunc(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	buf := bufferCloser{}
	router.logger = log.NewWriterLogger(&buf)
	data := &reverseproxy.RequestData{
		Host: "myfe.com",
	}
	err = router.EndRequest(data, false, func() *log.LogEntry { return &log.LogEntry{} })
	c.Assert(err, check.IsNil)
	members := s.redis.SMembers("dead:myfe.com").Val()
	c.Assert(members, check.DeepEquals, []string{})
	router.Stop()
	c.Assert(buf.String(), check.Equals, "::ffff: - - [01/Jan/0001:00:00:00 +0000] \"  \" 0 0 \"\" \"\" \":\" \"\" 0.000 0.000\n")
}

func (s *S) TestEndRequestWithError(c *check.C) {
	router := Router{}
	err := router.Init()
	c.Assert(err, check.IsNil)
	data := &reverseproxy.RequestData{
		Host: "myfe.com",
	}
	err = router.EndRequest(data, true, nil)
	c.Assert(err, check.IsNil)
	members := s.redis.SMembers("dead:myfe.com").Val()
	c.Assert(members, check.DeepEquals, []string{"0"})
}

func BenchmarkChooseBackend(b *testing.B) {
	r, err := redisConn()
	if err != nil {
		b.Fatal(err)
	}
	defer clearKeys(r)
	err = r.RPush("frontend:myfrontend.com", "myfrontend", "http://url1:123", "http://url2:123", "http://url3:123").Err()
	if err != nil {
		b.Fatal(err)
	}
	router := Router{}
	err = router.Init()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			router.ChooseBackend("myfrontend.com")
		}
	})
	b.StopTimer()
}

func BenchmarkChooseBackendNoCache(b *testing.B) {
	r, err := redisConn()
	if err != nil {
		b.Fatal(err)
	}
	defer clearKeys(r)
	err = r.RPush("frontend:myfrontend.com", "myfrontend", "http://url1:123", "http://url2:123", "http://url3:123").Err()
	if err != nil {
		b.Fatal(err)
	}
	router := Router{}
	err = router.Init()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			router.ChooseBackend("myfrontend.com")
			router.cache.Purge()
		}
	})
	b.StopTimer()
}

func BenchmarkChooseBackendManyNoCache(b *testing.B) {
	r, err := redisConn()
	if err != nil {
		b.Fatal(err)
	}
	defer clearKeys(r)
	backends := make([]string, 100)
	for i := range backends {
		backends[i] = "http://urlx:123"
	}
	backends = append([]string{"benchfrontend"}, backends...)
	err = r.RPush("frontend:myfrontend.com", backends...).Err()
	if err != nil {
		b.Fatal(err)
	}
	router := Router{}
	err = router.Init()
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			router.ChooseBackend("myfrontend.com")
			router.cache.Purge()
		}
	})
	b.StopTimer()
}
