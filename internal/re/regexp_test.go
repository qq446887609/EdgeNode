// Copyright 2022 Liuxiangchao iwind.liu@gmail.com. All rights reserved.

package re_test

import (
	"github.com/TeaOSLab/EdgeCommon/pkg/serverconfigs/firewallconfigs"
	"github.com/TeaOSLab/EdgeNode/internal/re"
	"github.com/iwind/TeaGo/assert"
	"regexp"
	"strings"
	"testing"
)

func TestRegexp(t *testing.T) {
	for _, s := range []string{"(?i)(abc|efg)", "abc|efg", "abc(.+)"} {
		var reg = regexp.MustCompile(s)
		t.Log("===" + s + "===")
		t.Log(reg.LiteralPrefix())
		t.Log(reg.NumSubexp())
		t.Log(reg.SubexpNames())
	}
}

func TestRegexp_MatchString(t *testing.T) {
	var a = assert.NewAssertion(t)

	{
		var r = re.MustCompile("abc")
		a.IsTrue(r.MatchString("abc"))
		a.IsFalse(r.MatchString("ab"))
		a.IsFalse(r.MatchString("ABC"))
	}

	{
		var r = re.MustCompile("(?i)abc|def|ghi")
		a.IsTrue(r.MatchString("DEF"))
		a.IsFalse(r.MatchString("ab"))
		a.IsTrue(r.MatchString("ABC"))
	}
}

func TestRegexp_Sub(t *testing.T) {
	{
		reg := regexp.MustCompile(`(a|b|c)(e|f|g)`)
		for _, subName := range reg.SubexpNames() {
			t.Log(subName)
		}
	}
}

func TestRegexp_ParseKeywords(t *testing.T) {
	var r = re.MustCompile("")

	{
		var keywords = r.ParseKeywords(`\n\t\n\f\r\v\x123`)
		t.Log(keywords)
	}
}

func TestRegexp_ParseKeywords2(t *testing.T) {
	var a = assert.NewAssertion(t)

	var r = re.MustCompile("")
	a.IsTrue(testCompareStrings(r.ParseKeywords("(abc)def"), []string{"abcdef"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("(abc)|(?:def)"), []string{"abc", "def"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("(abc)"), []string{"abc"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("(abc|def|ghi)"), []string{"abc", "def", "ghi"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("(?i:abc)"), []string{}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`\babc`), []string{"abc"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`    \babc`), []string{"    "}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`\babc\b`), []string{"abc"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`\b(abc)`), []string{"abc"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("abc"), []string{"abc"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("abc|efg|hij"), []string{"abc", "efg", "hij"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`abc\|efg|hij`), []string{"abc|efg", "hij"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`abc\|efg*|hij`), []string{"abc|ef", "hij"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`abc\|efg?|hij`), []string{"abc|ef", "hij"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`abc\|efg+|hij`), []string{"abc|ef", "hij"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`abc\|efg{2,10}|hij`), []string{"abc|ef", "hij"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`abc\|efg{0,10}|hij`), []string{"abc|ef", "hij"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`abc\|efg.+|hij`), []string{"abc|efg", "hij"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("A(abc|bcd)"), []string{"Aabc", "Abcd"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("^abc"), []string{"abc"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("abc$"), []string{"abc"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`abc$`), []string{"abc"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("abc\\d"), []string{"abc"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("abc{0,4}"), []string{"ab"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("{0,4}"), []string{}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("{1,4}"), []string{}))
	a.IsTrue(testCompareStrings(r.ParseKeywords("中文|北京|上海|golang"), []string{"中文", "北京", "上海", "golang"}))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`(onmouseover|onmousemove|onmousedown|onmouseup|onerror|onload|onclick|ondblclick)\s*=`), strings.Split("onmouseover|onmousemove|onmousedown|onmouseup|onerror|onload|onclick|ondblclick", "|")))
	a.IsTrue(testCompareStrings(r.ParseKeywords(`/\*(!|\x00)`), []string{"/*"}))
}

func TestRegexp_ParseKeywords3(t *testing.T) {
	var r = re.MustCompile("")

	var policy = firewallconfigs.HTTPFirewallTemplate()
	for _, group := range policy.Inbound.Groups {
		for _, set := range group.Sets {
			for _, rule := range set.Rules {
				if rule.Operator == firewallconfigs.HTTPFirewallRuleOperatorMatch || rule.Operator == firewallconfigs.HTTPFirewallRuleOperatorNotMatch {
					t.Log(set.Name+":", rule.Value, "=>", r.ParseKeywords(rule.Value))
				}
			}
		}
	}
}

func BenchmarkRegexp_MatchString(b *testing.B) {
	var r = re.MustCompile("(?i)(onmouseover|onmousemove|onmousedown|onmouseup|onerror|onload|onclick|ondblclick|onkeydown|onkeyup|onkeypress)(\\s|%09|%0A|(\\+|%20))*(=|%3D)")
	//b.Log("keywords:", r.Keywords())
	for i := 0; i < b.N; i++ {
		r.MatchString("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/96.0.4664.110 Safari/537.36")
	}
}

func BenchmarkRegexp_MatchString2(b *testing.B) {
	var r = regexp.MustCompile("(?i)(onmouseover|onmousemove|onmousedown|onmouseup|onerror|onload|onclick|ondblclick|onkeydown|onkeyup|onkeypress)(\\s|%09|%0A|(\\+|%20))*(=|%3D)")
	for i := 0; i < b.N; i++ {
		r.MatchString("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/96.0.4664.110 Safari/537.36")
	}
}

func BenchmarkRegexp_MatchString_CaseSensitive(b *testing.B) {
	var r = re.MustCompile("(abc|def|ghi)")
	b.Log("keywords:", r.Keywords())
	for i := 0; i < b.N; i++ {
		r.MatchString("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/96.0.4664.110 Safari/537.36")
	}
}

func testCompareStrings(s1 []string, s2 []string) bool {
	if len(s1) != len(s2) {
		return false
	}
	for index, s := range s1 {
		if s != s2[index] {
			return false
		}
	}
	return true
}
