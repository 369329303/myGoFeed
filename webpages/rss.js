
<script src="./jquery-3.5.1.min.js"></script>
$("#nytimes").click(function() {
    // alert(1);
    $.getJSON("./rss.html", {"FeedName": $("#nytimes").attr("name")});
    
    // alert("hello");
});
